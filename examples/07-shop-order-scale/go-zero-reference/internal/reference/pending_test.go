// 本文件验证独立 Badger 队列的持久恢复和重复请求边界，不依赖 Core 存储。
package reference

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/dgraph-io/badger/v3"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testOrder(id uint, request string) *Order {
	return &Order{ID: id, UserID: 720001, SupplierID: 2, ProductID: 3, RequestID: request, RequestFingerprint: request, UnitPrice: decimal.NewFromInt(21), Quantity: 2, TotalAmount: decimal.NewFromInt(42), OrderRevision: 1, OrderStatus: "accepted", PaymentStatus: "unpaid"}
}

// TestVerifiedOrphanDoesNotBlockQueue 验证先经远端事实核验的残留索引不会挡住后续订单。
func TestVerifiedOrphanDoesNotBlockQueue(t *testing.T) {
	s, e := openPending(t.TempDir())
	require.NoError(t, e)
	defer s.close()
	_, e = s.accept(context.Background(), testOrder(100, "healthy"))
	require.NoError(t, e)
	require.NoError(t, s.db.Update(func(tx *badger.Txn) error { return tx.Set([]byte("q:00000000000000000001"), []byte("p:missing")) }))
	verified := false
	s.verifyOrphans = func(q []queueOrphan) error {
		require.Len(t, q, 1)
		require.EqualValues(t, 1, q[0].id)
		verified = true
		return nil
	}
	items, e := s.batch(512)
	require.NoError(t, e)
	require.True(t, verified)
	require.Len(t, items, 1)
	require.NoError(t, s.confirm(items))
	require.Zero(t, s.depth.Load())
	require.NoError(t, s.db.View(func(tx *badger.Txn) error {
		_, e := tx.Get([]byte("q:00000000000000000001"))
		require.ErrorIs(t, e, badger.ErrKeyNotFound)
		return nil
	}))
}

// TestUnverifiedOrphanIsRetained 验证没有远端事实证明时保留索引并报告错误。
func TestUnverifiedOrphanIsRetained(t *testing.T) {
	s, e := openPending(t.TempDir())
	require.NoError(t, e)
	defer s.close()
	require.NoError(t, s.db.Update(func(tx *badger.Txn) error { return tx.Set([]byte("q:00000000000000000001"), []byte("p:missing")) }))
	_, e = s.batch(512)
	require.Error(t, e)
	require.NoError(t, s.db.View(func(tx *badger.Txn) error { _, e := tx.Get([]byte("q:00000000000000000001")); return e }))
}

// TestPendingConcurrentGroupCommit 验证并发合批逐条回执与真实队列条数一致。
func TestPendingConcurrentGroupCommit(t *testing.T) {
	s, e := openPending(t.TempDir())
	require.NoError(t, e)
	defer s.close()
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Go(func() {
			_, e := s.accept(context.Background(), testOrder(uint(1000+i), fmt.Sprintf("concurrent-%d", i)))
			assert.NoError(t, e)
		})
	}
	wg.Wait()
	items, e := s.batch(512)
	require.NoError(t, e)
	require.Len(t, items, 100)
	require.EqualValues(t, 100, s.depth.Load())
	require.NoError(t, s.confirm(items))
	require.Zero(t, s.depth.Load())
}

// TestPendingReopenAndIdempotency 验证成功接单已落盘、原路径恢复、同键不覆盖及冲突拒绝。
func TestPendingReopenAndIdempotency(t *testing.T) {
	path := t.TempDir()
	s, err := openPending(path)
	require.NoError(t, err)
	first, err := s.accept(context.Background(), testOrder(100, "a"))
	require.NoError(t, err)
	require.EqualValues(t, 100, first.ID)
	retry, err := s.accept(context.Background(), testOrder(101, "a"))
	require.NoError(t, err)
	require.Equal(t, first.ID, retry.ID)
	conflict := testOrder(102, "a")
	conflict.RequestFingerprint = "changed"
	_, err = s.accept(context.Background(), conflict)
	require.Error(t, err)
	require.NoError(t, s.close())
	s, err = openPending(path)
	require.NoError(t, err)
	defer s.close()
	items, err := s.batch(512)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.EqualValues(t, 100, items[0].ID)
	require.NoError(t, s.confirm(items))
	require.NoError(t, s.confirm(items))
	require.EqualValues(t, 0, s.depth.Load())
}

// TestPendingOtherKeySurvivesRepeatedConfirmation 防止旧 ACK 扣减其他订单的待处理计数。
func TestPendingOtherKeySurvivesRepeatedConfirmation(t *testing.T) {
	s, err := openPending(t.TempDir())
	require.NoError(t, err)
	defer s.close()
	a, err := s.accept(context.Background(), testOrder(100, "a"))
	require.NoError(t, err)
	require.NoError(t, s.confirm([]*Order{a}))
	_, err = s.accept(context.Background(), testOrder(101, "b"))
	require.NoError(t, err)
	require.NoError(t, s.confirm([]*Order{a}))
	require.EqualValues(t, 1, s.depth.Load())
	items, err := s.batch(512)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "b", items[0].RequestID)
}

// TestNativeOrderLanesUseAllWorkers 验证低位相同的 Snowflake 订单仍能分布到所有事件 worker。
func TestNativeOrderLanesUseAllWorkers(t *testing.T) {
	for _, workers := range []uint{4, 8} {
		counts := make([]int, workers)
		for i := range 8000 {
			counts[orderLane(uint(1700000000+i)<<22, workers)]++
		}
		for lane, count := range counts {
			require.InDelta(t, 8000/int(workers), count, 8000/float64(workers)*0.15, "workers=%d lane=%d counts=%v", workers, lane, counts)
		}
	}
}
