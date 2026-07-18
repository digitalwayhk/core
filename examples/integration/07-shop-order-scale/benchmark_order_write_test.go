// Package shoporderscale 提供 07 订单水平扩展示例的写入基准测试。
// 基准聚焦本地可靠 pending 接收与远程同步分离后的热路径，便于和 06 同步下单路径做趋势对比。
package shoporderscale

import (
	"context"
	"fmt"
	"testing"
	"time"

	orderbusiness "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/business"
	ordermodels "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// Benchmark07LocalOrderAccept 测量 07 下单入口只写本地 pending 的接收成本。
func Benchmark07LocalOrderAccept(b *testing.B) {
	require.NoError(b, ordermodels.EnsureStorage())
	writer := orderbusiness.LocalOrderWriter{}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		requestID := fmt.Sprintf("bench-07-local-%d-%d", time.Now().UnixNano(), index)
		orderID := uint(time.Now().UnixNano()%1_000_000_000) + uint(index) + 900000000
		_, err := writer.Accept(context.Background(), orderbusiness.CreateOrderCommand{
			OrderID:            orderID,
			UserID:             uint(100000000 + index),
			RequestID:          requestID,
			RequestFingerprint: requestID,
			SupplierID:         200001,
			ProductID:          300001,
			UnitPrice:          decimal.NewFromInt(10),
			Quantity:           2,
			TraceID:            "bench-trace-" + requestID,
			ServiceName:        "shop-order",
			ServiceInstanceID:  "bench-order",
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmark07DrainPendingOnce 测量 07 从本地 pending 同步到远程权威库的批量成本。
func Benchmark07DrainPendingOnce(b *testing.B) {
	require.NoError(b, ordermodels.EnsureStorage())
	writer := orderbusiness.LocalOrderWriter{}
	for index := 0; index < b.N; index++ {
		requestID := fmt.Sprintf("bench-07-drain-%d-%d", time.Now().UnixNano(), index)
		orderID := uint(time.Now().UnixNano()%1_000_000_000) + uint(index) + 920000000
		_, err := writer.Accept(context.Background(), orderbusiness.CreateOrderCommand{
			OrderID:            orderID,
			UserID:             uint(120000000 + index),
			RequestID:          requestID,
			RequestFingerprint: requestID,
			SupplierID:         220001,
			ProductID:          320001,
			UnitPrice:          decimal.NewFromInt(10),
			Quantity:           2,
			TraceID:            "bench-trace-" + requestID,
			ServiceName:        "shop-order",
			ServiceInstanceID:  "bench-order",
		})
		require.NoError(b, err)
	}
	syncer := orderbusiness.RemoteOrderSyncer{}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if err := syncer.DrainOnce(context.Background(), 1); err != nil {
			b.Fatal(err)
		}
	}
}
