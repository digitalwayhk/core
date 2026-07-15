package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestGetOrderWriteStoreUsesAtomicFastPath 确保服务启动后的每次下单不再获取生命周期全局锁。
// 测试有意持有 globalOrderWriteStoreMu；若热路径退化为旧实现，读取将被阻塞。
func TestGetOrderWriteStoreUsesAtomicFastPath(t *testing.T) {
	store := &OrderWriteStore{}
	previous := activeOrderWriteStore.Swap(store)
	t.Cleanup(func() { activeOrderWriteStore.Store(previous) })

	globalOrderWriteStoreMu.Lock()
	result := make(chan *OrderWriteStore, 1)
	errors := make(chan error, 1)
	go func() {
		loaded, err := getOrderWriteStore()
		result <- loaded
		errors <- err
	}()

	select {
	case loaded := <-result:
		globalOrderWriteStoreMu.Unlock()
		require.Same(t, store, loaded)
		require.NoError(t, <-errors)
	case <-time.After(200 * time.Millisecond):
		globalOrderWriteStoreMu.Unlock()
		t.Fatal("订单存储热路径不应等待全局生命周期锁")
	}
}
