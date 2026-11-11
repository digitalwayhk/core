// 本文件以真实可靠store验证同事务新快照免回读、规范指纹及版本隔离，不连接消费方环境。
package nosql

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/dgraph-io/badger/v4"
	"github.com/stretchr/testify/require"
)

// TestTransactionSnapshotReuseAvoidsReadback 固定已在当前事务构造的新版本不应再次完整回读解码。
func TestTransactionSnapshotReuseAvoidsReadback(t *testing.T) {
	store := groupOperationsStore(t, t.TempDir(), false)
	reads := 0
	store.db.disposition.snapshotReadObserver = func() { reads++ }
	items := []*confirmationLoadRecord{groupOperationRecord("one", 1, 2028), groupOperationRecord("two", 2, 2028)}
	result, err := store.SaveBatch(context.Background(), items)
	require.NoError(t, err)
	require.Equal(t, 2, result.Committed)
	require.Zero(t, reads, "当前事务已持有新wrapper，不应再txn.Get+Unmarshal同一版本")
	require.Equal(t, 2, store.Metrics().Pending)
}

// TestTransactionSnapshotReuseCanonicalIdentity 用旧版读回+标准JSON算法逐字固定FP和ID，不从新路径生成期望。
func TestTransactionSnapshotReuseCanonicalIdentity(t *testing.T) {
	store := groupOperationsStore(t, t.TempDir(), false)
	for i := range 3 {
		value := groupOperationRecord("same", i+1, 2028+i)
		require.NoError(t, store.Save(context.Background(), value))
		record := currentGroupRecord(t, store, "same")
		require.NoError(t, store.db.manager.db.View(func(txn *badger.Txn) error {
			var snapshot SyncQueueItem[confirmationLoadRecord]
			if err := dispositionRead(txn, store.db.prefix+"same", &snapshot); err != nil {
				return err
			}
			canonical, err := json.Marshal(&snapshot)
			if err != nil {
				return err
			}
			actual, err := json.Marshal(record.Snapshot)
			if err != nil {
				return err
			}
			require.Equal(t, string(canonical), string(actual))
			fp := dispositionHash(string(canonical))
			require.Equal(t, fp, record.Fingerprint)
			require.Equal(t, dispositionHash(store.db.disposition.policy.StoreID+"\x00"+store.db.prefix+"\x00"+snapshot.Key+"\x00"+fp), record.ID)
			return nil
		}))
		value.Payload[0] = 0
		require.Equal(t, byte('x'), record.Snapshot.Item.Payload[0], "持久原件不能与Save参数共享可变载荷")
	}
}

// TestTransactionSnapshotReuseSameKeyMultipleWrites 固定同批多次Save/改组，不能用末次wrapper覆盖旧责任。
func TestTransactionSnapshotReuseSameKeyMultipleWrites(t *testing.T) {
	store := groupOperationsStore(t, t.TempDir(), false)
	items := []*confirmationLoadRecord{groupOperationRecord("same", 1, 10), groupOperationRecord("same", 2, 20), groupOperationRecord("same", 3, 30)}
	result, err := store.SaveBatch(context.Background(), items)
	require.NoError(t, err)
	require.Equal(t, 3, result.Committed)
	records, _, err := store.ListWriteBehindFailures("", 10)
	require.NoError(t, err)
	require.Len(t, records, 3)
	counts := map[string]int{}
	for _, r := range records {
		counts[r.Group]++
		if r.Group == "3" {
			require.Equal(t, WriteBehindReady, r.State)
			require.Len(t, r.Snapshot.Item.Payload, 30)
		} else {
			require.Equal(t, WriteBehindSuperseded, r.State)
		}
	}
	require.Equal(t, map[string]int{"1": 1, "2": 1, "3": 1}, counts)
	require.Equal(t, 1, store.Metrics().Pending)
	requireDispositionGroup(t, store.db, 1, false)
	requireDispositionGroup(t, store.db, 2, false)
	requireDispositionGroup(t, store.db, 3, true)
}

// BenchmarkTransactionSnapshotReuse 在真实Badger上固定128项/2028字节新建，计时外建库；确认与排空另作持续实验。
func BenchmarkTransactionSnapshotReuse(b *testing.B) {
	t := &confirmationLoadTarget{}
	path := b.TempDir()
	cfg := DefaultProductionConfig(path)
	cfg.AutoSync = false
	cfg.EnableLogger = false
	cfg.WriteBehindFailure = &WriteBehindFailurePolicy{StoreID: "snapshot-bench", MaxAttempts: 3, MinRetryInterval: 1000000000, MaxRetryInterval: 10000000000, HandlerMaxAttempts: 2, HandlerTimeout: 1000000000, HandlerWorkers: 1, ScanLimit: 128}
	db, err := NewSharedBadgerDB[confirmationLoadRecord](path, cfg)
	if err != nil {
		b.Fatal(err)
	}
	if err = db.UseWriteBehindWithFailureHandler(t, t); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = db.Close(); _ = CloseSharedManager(path) })
	b.ReportAllocs()
	b.ResetTimer()
	for n := range b.N {
		ops := make([]WriteOperation[confirmationLoadRecord], 128)
		for i := range ops {
			ops[i] = WriteOperation[confirmationLoadRecord]{Type: WriteOperationSave, Item: groupOperationRecord(fmt.Sprintf("%d-%d", n, i), i%8, 2028)}
		}
		if r, e := db.ApplyWriteOperations(ops); e != nil || r.Committed != 128 {
			b.Fatalf("committed=%d error=%v", r.Committed, e)
		}
	}
}
