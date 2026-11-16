// 本文件覆盖普通确认读证明失效、旧版本、坏原件、事务失败与原codec回退，不使用业务卷。
package nosql

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/stretchr/testify/require"
)

type plainConfirmationFunc func(context.Context, []*SyncQueueItem[plainConfirmationRecord]) (*WriteBehindResult, error)

// SyncBatch 委托测试控制真实target结果；本地确认仍使用生产实现。
func (fn plainConfirmationFunc) SyncBatch(ctx context.Context, rows []*SyncQueueItem[plainConfirmationRecord]) (*WriteBehindResult, error) {
	return fn(ctx, rows)
}

func mutatePlainConfirmation(t *testing.T, db *PrefixedBadgerDB[plainConfirmationRecord], key string, change func([]byte) []byte) {
	t.Helper()
	require.NoError(t, db.manager.db.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}
		return item.Value(func(b []byte) error { return txn.Set([]byte(key), change(bytes.Clone(b))) })
	}))
}

// TestPlainConfirmationOldResultCannotConfirmNewSave 新Save使证明失效，原CAS保留pending和新内容。
func TestPlainConfirmationOldResultCannotConfirmNewSave(t *testing.T) {
	store := openPlainConfirmationStore(t)
	rows, err := store.db.getUnsyncedBatch(8)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NoError(t, store.Save(context.Background(), &plainConfirmationRecord{Model: entity.NewModel(), Key: "one", Value: plainConfirmationValue{Data: "new"}}))
	plainConfirmationDecodes.Store(0)
	current, err := store.db.currentSyncItems(rows)
	require.NoError(t, err)
	require.Empty(t, current)
	require.Positive(t, plainConfirmationDecodes.Load())
	confirmed, err := store.db.syncBatchWithTargetContext(context.Background(), rows, plainConfirmationTarget{t})
	require.NoError(t, err)
	require.Empty(t, confirmed)
	wrapper, err := store.db.getWrapper(rows[0].Key)
	require.NoError(t, err)
	require.Equal(t, "new", wrapper.Item.Value.Data)
	require.False(t, wrapper.IsSynced)
	require.Equal(t, 1, store.db.GetCachedPendingSyncCount())
}

// TestPlainConfirmationChangedBadOriginal 证明不是跳过坏原件检查的许可；变化后完整codec仍拒绝。
func TestPlainConfirmationChangedBadOriginal(t *testing.T) {
	store := openPlainConfirmationStore(t)
	rows, err := store.db.getUnsyncedBatch(8)
	require.NoError(t, err)
	mutatePlainConfirmation(t, store.db, rows[0].Key, func(b []byte) []byte { return bytes.Replace(b, []byte(`"data":"original"`), []byte(`"data":42`), 1) })
	current, err := store.db.currentSyncItems(rows)
	require.Error(t, err)
	require.Empty(t, current)
	confirmed, err := store.db.syncBatchWithTargetContext(context.Background(), rows, plainConfirmationTarget{t})
	require.NoError(t, err)
	require.Empty(t, confirmed)
	require.Equal(t, 1, store.db.GetCachedPendingSyncCount())
}

// TestPlainConfirmationCodecFallback 保留无证明、重复/别名封套的原解码/重编码语义。
func TestPlainConfirmationCodecFallback(t *testing.T) {
	for _, mode := range []string{"no_proof", "alias", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			store := openPlainConfirmationStore(t)
			rows, err := store.db.getUnsyncedBatch(8)
			require.NoError(t, err)
			key := rows[0].Key
			if mode != "no_proof" {
				mutatePlainConfirmation(t, store.db, key, func(b []byte) []byte {
					if mode == "alias" {
						return bytes.Replace(b, []byte(`"is_synced"`), []byte(`"IS_SYNCED"`), 1)
					}
					return []byte(strings.TrimSuffix(string(b), "}") + `,"is_synced":false}`)
				})
				rows, err = store.db.getUnsyncedBatch(8)
				require.NoError(t, err)
				require.Len(t, rows, 1)
			} else {
				rows[0].readProof = nil
			}
			plainConfirmationDecodes.Store(0)
			plainConfirmationEncodes.Store(0)
			confirmed, err := store.db.syncBatchWithTargetContext(context.Background(), rows, plainConfirmationTarget{t})
			require.NoError(t, err)
			require.Len(t, confirmed, 1)
			require.Positive(t, plainConfirmationDecodes.Load())
			require.Positive(t, plainConfirmationEncodes.Load())
			require.Zero(t, store.db.GetCachedPendingSyncCount())
		})
	}
}

// TestPlainConfirmationPartialUnknownAndDuplicate 只有真实确认项移除pending，重复结果不能重复扣数。
func TestPlainConfirmationPartialUnknownAndDuplicate(t *testing.T) {
	store := openPlainConfirmationStore(t)
	require.NoError(t, store.Save(context.Background(), &plainConfirmationRecord{Model: entity.NewModel(), Key: "two", Value: plainConfirmationValue{Data: "original"}}))
	rows, err := store.db.getUnsyncedBatch(8)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	uncertain := errors.New("Core合成target部分结果未知")
	target := plainConfirmationFunc(func(_ context.Context, items []*SyncQueueItem[plainConfirmationRecord]) (*WriteBehindResult, error) {
		return &WriteBehindResult{ConfirmedKeys: []string{items[0].Key, items[0].Key}}, uncertain
	})
	confirmed, err := store.db.syncBatchWithTargetContext(context.Background(), rows, target)
	require.ErrorIs(t, err, uncertain)
	require.Len(t, confirmed, 1)
	require.Equal(t, 1, store.db.GetCachedPendingSyncCount())
	confirmed, err = store.db.syncBatchWithTargetContext(context.Background(), rows, target)
	require.ErrorIs(t, err, uncertain)
	require.Empty(t, confirmed)
	require.Equal(t, 1, store.db.GetCachedPendingSyncCount())
}

// TestPlainConfirmationLocalFailureDoesNotConfirm 目标成功不替代本地提交，真实关库错误不扣pending。
func TestPlainConfirmationLocalFailureDoesNotConfirm(t *testing.T) {
	store := openPlainConfirmationStore(t)
	rows, err := store.db.getUnsyncedBatch(8)
	require.NoError(t, err)
	// 通过现有manager生命周期关库，避免夹具直接关底层DB后再次SyncWAL。
	require.NoError(t, CloseSharedManager(store.config.BasePath))
	confirmed, err := store.db.syncBatchWithTargetContext(context.Background(), rows, plainConfirmationTarget{t})
	require.Error(t, err)
	require.Empty(t, confirmed)
	require.Equal(t, 1, store.db.GetCachedPendingSyncCount())
}

// TestPlainConfirmationProofCannotOverrideSnapshotVersion 修改公有快照时间不能让私有证明承认另一个版本。
func TestPlainConfirmationProofCannotOverrideSnapshotVersion(t *testing.T) {
	store := openPlainConfirmationStore(t)
	rows, err := store.db.getUnsyncedBatch(8)
	require.NoError(t, err)
	rows[0].UpdatedAt = rows[0].UpdatedAt.Add(time.Second)
	confirmed, err := store.db.syncBatchWithTargetContext(context.Background(), rows, plainConfirmationTarget{t})
	require.NoError(t, err)
	require.Empty(t, confirmed)
	require.Equal(t, 1, store.db.GetCachedPendingSyncCount())
}
