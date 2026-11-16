// 本文件检验读证明的数据库/key/MVCC范围及JSON封套边界，不放宽原codec回退。
package nosql

import (
	"bytes"
	"context"
	stdjson "encoding/json"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/stretchr/testify/require"
)

// TestPlainConfirmationProofIsStoreAndKeyBound 同库同提交版本的不同key及另一个DB均不能借用证明。
func TestPlainConfirmationProofIsStoreAndKeyBound(t *testing.T) {
	store := openPlainConfirmationStore(t)
	result, err := store.SaveBatch(context.Background(), []*plainConfirmationRecord{{Model: entity.NewModel(), Key: "one", Value: plainConfirmationValue{Data: "original"}}, {Model: entity.NewModel(), Key: "two", Value: plainConfirmationValue{Data: "original"}}})
	require.NoError(t, err)
	require.Equal(t, 2, result.Committed)
	rows, err := store.db.getUnsyncedBatch(8)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	a, b := rows[0], rows[1]
	require.Equal(t, a.readProof.version, b.readProof.version, "合批中不同key可共享Badger版本号")
	require.False(t, a.readProof.matches(store.db.manager.db, b.Key, a.readProof.version, a.UpdatedAt, nil))
	other := openPlainConfirmationStore(t)
	require.False(t, a.readProof.matches(other.db.manager.db, a.Key, a.readProof.version, a.UpdatedAt, nil))
	plainConfirmationDecodes.Store(0)
	a.readProof = b.readProof
	current, err := store.db.currentSyncItems([]*SyncQueueItem[plainConfirmationRecord]{a})
	require.NoError(t, err)
	require.Len(t, current, 1)
	require.Positive(t, plainConfirmationDecodes.Load(), "错误范围的证明必须回到完整codec")
}

// TestPlainConfirmationRewrittenBytesUseOriginalCodec 即使内容逐字相同，新提交的MVCC版本也保守回退。
func TestPlainConfirmationRewrittenBytesUseOriginalCodec(t *testing.T) {
	store := openPlainConfirmationStore(t)
	rows, err := store.db.getUnsyncedBatch(8)
	require.NoError(t, err)
	version := rows[0].readProof.version
	mutatePlainConfirmation(t, store.db, rows[0].Key, func(b []byte) []byte { return b })
	require.NoError(t, store.db.manager.db.View(func(txn *badger.Txn) error {
		item, e := txn.Get([]byte(rows[0].Key))
		if e != nil {
			return e
		}
		require.NotEqual(t, version, item.Version())
		return nil
	}))
	plainConfirmationDecodes.Store(0)
	confirmed, err := store.db.syncBatchWithTargetContext(context.Background(), rows, plainConfirmationTarget{t})
	require.NoError(t, err)
	require.Len(t, confirmed, 1)
	require.Positive(t, plainConfirmationDecodes.Load())
	require.Zero(t, store.db.GetCachedPendingSyncCount())
}

// TestPlainConfirmationEnvelopeFraming 成熟JSON遍历只替换技术字段值，不替换正文中的同名文本。
func TestPlainConfirmationEnvelopeFraming(t *testing.T) {
	now := time.Date(2026, 11, 16, 0, 0, 0, 123, time.UTC)
	for _, encoded := range []string{`{"item":{"text":"is_synced:false","future":[1,2]},"is_synced":false}`, `{ "item" : { "opaque" : 1.234567890123456789 }, "" : true, "future":null }`} {
		result, ok := markPlainSyncEnvelope([]byte(encoded), now)
		require.True(t, ok)
		require.True(t, stdjson.Valid(result))
		var before, after map[string]stdjson.RawMessage
		require.NoError(t, stdjson.Unmarshal([]byte(encoded), &before))
		require.NoError(t, stdjson.Unmarshal(result, &after))
		require.True(t, bytes.Equal(before["item"], after["item"]))
		require.Equal(t, "true", string(after["is_synced"]))
		for key, value := range before {
			if key != "is_synced" && key != "synced_at" {
				require.Equal(t, value, after[key])
			}
		}
	}
	for _, encoded := range []string{`{"is_synced":false,"is_synced":true}`, `{"IS_SYNCED":false}`, `{"item":1} garbage`, `null`, `[]`, `{"item":`} {
		_, ok := markPlainSyncEnvelope([]byte(encoded), now)
		require.False(t, ok, encoded)
	}
}

// TestPlainConfirmationActualEncodingStillChecked 相同MVCC身份也必须核对实际编码，指纹不符回到原codec。
func TestPlainConfirmationActualEncodingStillChecked(t *testing.T) {
	store := openPlainConfirmationStore(t)
	rows, err := store.db.getUnsyncedBatch(8)
	require.NoError(t, err)
	proof := rows[0].readProof
	require.NoError(t, store.db.manager.db.View(func(txn *badger.Txn) error {
		item, e := txn.Get([]byte(rows[0].Key))
		if e != nil {
			return e
		}
		return item.Value(func(b []byte) error {
			require.True(t, proof.matches(store.db.manager.db, rows[0].Key, item.Version(), rows[0].UpdatedAt, b))
			return nil
		})
	}))
	proof.encodingHash[0] ^= 0xff
	plainConfirmationDecodes.Store(0)
	current, err := store.db.currentSyncItems(rows)
	require.NoError(t, err)
	require.Len(t, current, 1)
	require.Positive(t, plainConfirmationDecodes.Load(), "不能仅凭MVCC版本跳过实际字节核验")
}
