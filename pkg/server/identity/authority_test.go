// 本文件验证编号权威的原子分配、启动世代和时间上界。
package identity

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGeneratorIdentityAtomicAllocation 验证跨服务按真实 worker 排他绑定。
func TestGeneratorIdentityAtomicAllocation(t *testing.T) {
	a := NewAuthority(NewMemoryStore(), "epoch-a")
	ctx := context.Background()
	require.NoError(t, a.Initialize(ctx))
	var wg sync.WaitGroup
	ids := make(chan uint16, 10)
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b, err := a.Bind(ctx, "orders", fmt.Sprint(i), 1, 0, 31, true)
			require.NoError(t, err)
			ids <- b.WorkerID
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[uint16]bool{}
	for id := range ids {
		require.False(t, seen[id])
		seen[id] = true
	}
	_, err := a.Bind(ctx, "other", "a", 0, 10, 31, false)
	require.Error(t, err, "不能把不同 DC/Machine 映射到已占用的 worker 10")
	b, err := a.Bind(ctx, "orders", "0", 1, 0, 31, true)
	require.NoError(t, err)
	_, err = a.Bind(ctx, "new", "full", 1, 0, 31, true)
	require.Error(t, err)
	require.NotNil(t, b)
}

// TestGeneratorBootAndReservationFence 验证旧世代不能预留时间或撤销新世代。
func TestGeneratorBootAndReservationFence(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	a := NewAuthority(store, "epoch-a")
	require.ErrorIs(t, a.Check(ctx), ErrAuthorityMissing)
	require.NoError(t, a.Initialize(ctx))
	b, err := a.Bind(ctx, "orders", "a", 1, 0, 31, false)
	require.NoError(t, err)
	first, err := a.AcquireBoot(ctx, b.Key, "boot-a", "", false)
	require.NoError(t, err)
	_, err = a.AcquireBoot(ctx, b.Key, "boot-b", "boot-a", false)
	require.Error(t, err)
	require.NoError(t, a.Reserve(ctx, b.Key, "boot-a", first, 100))
	require.NoError(t, a.ReleaseBoot(ctx, b.Key, "boot-a", first))
	second, err := a.AcquireBoot(ctx, b.Key, "boot-b", "boot-a", false)
	require.NoError(t, err)
	require.Greater(t, second, first)
	require.Error(t, a.Reserve(ctx, b.Key, "boot-a", first, 200))
	require.Error(t, a.ReleaseBoot(ctx, b.Key, "boot-a", first))
	current, err := a.Get(ctx, b.Key)
	require.NoError(t, err)
	require.Equal(t, int64(100), current.ReservedUntil)
	require.Error(t, NewAuthority(store, "epoch-b").Check(ctx))
}

// TestAuthorityRejectsCorruptLedger 验证损坏反向索引不能使既有编号被重新分配。
func TestAuthorityRejectsCorruptLedger(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	a := NewAuthority(store, "epoch-a")
	require.NoError(t, a.Initialize(ctx))
	_, err := a.Bind(ctx, "orders", "a", 1, 0, 31, false)
	require.NoError(t, err)
	raw, err := store.Load(ctx)
	require.NoError(t, err)
	var l ledger
	require.NoError(t, json.Unmarshal([]byte(raw), &l))
	delete(l.Workers, "10")
	bad, err := json.Marshal(l)
	require.NoError(t, err)
	ok, err := store.CompareAndSwap(ctx, raw, string(bad))
	require.NoError(t, err)
	require.True(t, ok)
	require.ErrorIs(t, a.Check(ctx), ErrAuthorityInvalid)
	_, err = a.Bind(ctx, "other", "b", 0, 10, 31, false)
	require.Error(t, err)
}
