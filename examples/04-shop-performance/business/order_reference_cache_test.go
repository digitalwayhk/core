package business

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// TestOrderReferenceCacheInvalidatesThroughEventBridge 演示下单事实缓存的完整路径：
// 同一商品只读一次持久层，Manage 变更发布控制事件后下一次读取必须重新加载。
func TestOrderReferenceCacheInvalidatesThroughEventBridge(t *testing.T) {
	bridge := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, bridge.Close(ctx))
	})

	var loads atomic.Int32
	cache, err := newOrderReferenceCache(bridge, func(productID uint) (*orderReferenceSnapshot, error) {
		loads.Add(1)
		return &orderReferenceSnapshot{
			ProductID:    productID,
			ProductCode:  "product",
			ProductName:  "演示商品",
			SupplierID:   2,
			SupplierCode: "supplier",
			SupplierName: "演示供应商",
			UnitPrice:    decimal.RequireFromString("12.50"),
		}, nil
	})
	require.NoError(t, err)
	t.Cleanup(cache.Close)

	first, err := cache.Get(1)
	require.NoError(t, err)
	second, err := cache.Get(1)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Equal(t, int32(1), loads.Load(), "相同商品不应重复读取 SQLite")

	require.NoError(t, cache.Invalidate(context.Background()))
	_, err = cache.Get(1)
	require.NoError(t, err)
	require.Equal(t, int32(2), loads.Load(), "EventBridge 控制事件后必须重新加载")
}

// TestOrderReferenceCacheDoesNotRestoreStaleLoad 确保失效事件与冷加载并发时，
// 失效前已开始的迟到加载不会在回调清空之后重新写回旧快照。
func TestOrderReferenceCacheDoesNotRestoreStaleLoad(t *testing.T) {
	bridge := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, bridge.Close(ctx))
	})

	firstLoadEntered := make(chan struct{})
	releaseFirstLoad := make(chan struct{})
	var loads atomic.Int32
	cache, err := newOrderReferenceCache(bridge, func(productID uint) (*orderReferenceSnapshot, error) {
		call := loads.Add(1)
		if call == 1 {
			close(firstLoadEntered)
			<-releaseFirstLoad
		}
		return &orderReferenceSnapshot{ProductID: productID, ProductName: "snapshot"}, nil
	})
	require.NoError(t, err)
	t.Cleanup(cache.Close)

	firstDone := make(chan error, 1)
	go func() {
		_, getErr := cache.Get(1)
		firstDone <- getErr
	}()
	<-firstLoadEntered
	require.NoError(t, cache.Invalidate(context.Background()))
	close(releaseFirstLoad)
	require.NoError(t, <-firstDone)

	_, err = cache.Get(1)
	require.NoError(t, err)
	require.Equal(t, int32(2), loads.Load(), "失效前开始的旧加载不得重新写入缓存")
}
