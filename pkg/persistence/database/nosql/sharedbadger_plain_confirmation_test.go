// 本文件以真实普通ReliableWriteStore和Badger验证确认不重复构造业务T及原封套兼容。
package nosql

import (
	"context"
	stdjson "encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	corejson "github.com/digitalwayhk/core/pkg/json"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/stretchr/testify/require"
)

var plainConfirmationDecodes atomic.Int64
var plainConfirmationEncodes atomic.Int64

type plainConfirmationValue struct {
	Data string `json:"data"`
}

// MarshalJSON 记录实际业务codec调用，元数据确认不应重复编码该值。
func (v plainConfirmationValue) MarshalJSON() ([]byte, error) {
	plainConfirmationEncodes.Add(1)
	type wire plainConfirmationValue
	return stdjson.Marshal(wire(v))
}

// UnmarshalJSON 首次选批必须解析真实T；原件变化后仍须走原codec校验。
func (v *plainConfirmationValue) UnmarshalJSON(b []byte) error {
	plainConfirmationDecodes.Add(1)
	type wire plainConfirmationValue
	return stdjson.Unmarshal(b, (*wire)(v))
}

type plainConfirmationRecord struct {
	*entity.Model
	Key   string
	Value plainConfirmationValue
}

// NewModel 初始化原模型基座，不引入业务状态。
func (r *plainConfirmationRecord) NewModel() {
	if r.Model == nil {
		r.Model = entity.NewModel()
	}
}

// GetLocalKey 提供测试记录自己的稳定身份。
func (r *plainConfirmationRecord) GetLocalKey() string { return r.Key }

type plainConfirmationTarget struct{ t *testing.T }

// SyncBatch 接收实际业务T后才确认，绝不以JSON占位替代目标输入。
func (target plainConfirmationTarget) SyncBatch(_ context.Context, items []*SyncQueueItem[plainConfirmationRecord]) (*WriteBehindResult, error) {
	result := &WriteBehindResult{}
	for _, item := range items {
		require.NotNil(target.t, item.Item)
		require.Equal(target.t, "original", item.Item.Value.Data)
		result.ConfirmedKeys = append(result.ConfirmedKeys, item.Key)
	}
	return result, nil
}

func openPlainConfirmationStore(t *testing.T) *ReliableWriteStore[plainConfirmationRecord] {
	t.Helper()
	base := t.TempDir()
	cfg := DefaultProductionConfig(base)
	cfg.AutoSync = false
	cfg.EnableLogger = false
	store, _, err := NewReliableWriteStore[plainConfirmationRecord](ServiceIdentity{ServiceName: "plain-confirm", DataCenterID: 1, MachineID: 1}, ReliableWriteStoreConfig{BasePath: base, Badger: cfg, CloseTimeout: time.Second})
	require.NoError(t, err)
	require.NoError(t, store.UseWriteBehind(plainConfirmationTarget{t}))
	t.Cleanup(func() { _ = store.Close(context.Background()); _ = CloseSharedManager(store.config.BasePath) })
	require.NoError(t, store.Save(context.Background(), &plainConfirmationRecord{Model: entity.NewModel(), Key: "one", Value: plainConfirmationValue{Data: "original"}}))
	return store
}

// TestPlainConfirmationReusesValidatedItem 真实选批已验证原件不变时，复核与确认均不再构造/编码T。
func TestPlainConfirmationReusesValidatedItem(t *testing.T) {
	for _, phase := range []string{"recheck", "confirmation"} {
		t.Run(phase, func(t *testing.T) {
			store := openPlainConfirmationStore(t)
			rows, err := store.db.getUnsyncedBatch(8)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			plainConfirmationDecodes.Store(0)
			plainConfirmationEncodes.Store(0)
			if phase == "recheck" {
				current, err := store.db.currentSyncItems(rows)
				require.NoError(t, err)
				require.Len(t, current, 1)
			} else {
				confirmed, err := store.db.syncBatchWithTargetContext(context.Background(), rows, plainConfirmationTarget{t})
				require.NoError(t, err)
				require.Len(t, confirmed, 1)
				require.Zero(t, store.db.GetCachedPendingSyncCount())
			}
			require.Zero(t, plainConfirmationDecodes.Load(), "不能重复解码已验证且未变的业务T")
			require.Zero(t, plainConfirmationEncodes.Load(), "不能为技术确认重新编码业务T")
		})
	}
}

// TestPlainConfirmationPreservesOpaqueFields 真实codec仍接收T，确认保留未知字段及原item编码。
func TestPlainConfirmationPreservesOpaqueFields(t *testing.T) {
	store := openPlainConfirmationStore(t)
	rows, err := store.db.getUnsyncedBatch(8)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	key := rows[0].Key
	var originalItem stdjson.RawMessage
	require.NoError(t, store.db.manager.db.Update(func(txn *badger.Txn) error {
		entry, e := txn.Get([]byte(key))
		if e != nil {
			return e
		}
		return entry.Value(func(b []byte) error {
			var fields map[string]stdjson.RawMessage
			if e := stdjson.Unmarshal(b, &fields); e != nil {
				return e
			}
			var item map[string]stdjson.RawMessage
			if e := stdjson.Unmarshal(fields["item"], &item); e != nil {
				return e
			}
			item["Value"] = stdjson.RawMessage(`{"data":"original","future_item":17}`)
			originalItem, e = corejson.Marshal(item)
			if e != nil {
				return e
			}
			fields["item"] = originalItem
			fields["future_wrapper"] = stdjson.RawMessage(`{"keep":true}`)
			b, e = corejson.Marshal(fields)
			if e != nil {
				return e
			}
			return txn.Set([]byte(key), b)
		})
	}))
	result, err := store.ForceSyncBatch(context.Background(), 8)
	require.NoError(t, err)
	require.Equal(t, 1, result.Confirmed)
	require.NoError(t, store.db.manager.db.View(func(txn *badger.Txn) error {
		entry, e := txn.Get([]byte(key))
		if e != nil {
			return e
		}
		return entry.Value(func(b []byte) error {
			var fields map[string]stdjson.RawMessage
			require.NoError(t, stdjson.Unmarshal(b, &fields))
			require.JSONEq(t, `{"keep":true}`, string(fields["future_wrapper"]))
			require.Equal(t, []byte(originalItem), []byte(fields["item"]), "不能丢失业务codec未消费的未来字段")
			return nil
		})
	}))
}
