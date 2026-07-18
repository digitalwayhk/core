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
	for _, concurrency := range benchmarkConcurrencies() {
		concurrency := concurrency
		b.Run(fmt.Sprintf("concurrency-%d", concurrency), func(b *testing.B) {
			b.Setenv("SHOP_LOCAL_PENDING_DIR", b.TempDir())
			require.NoError(b, ordermodels.StartOrderWriteStore())
			b.Cleanup(func() { require.NoError(b, ordermodels.StopOrderWriteStore()) })
			commands := make07OrderCommands("bench-07-local", newBenchmarkIDFactory(21), 100000000, b.N)
			writer := orderbusiness.LocalOrderWriter{}
			runBusinessBenchmarkSingleConcurrency(b, concurrency, "orders/s", func(index int) error {
				_, err := writer.Accept(context.Background(), commands[index])
				return err
			})
		})
	}
}

// Benchmark07DrainPendingOnce 测量 07 从本地 pending 同步到远程权威库的批量成本。
func Benchmark07DrainPendingOnce(b *testing.B) {
	requireOrderMySQL(b)
	b.Setenv("SHOP_LOCAL_PENDING_DIR", b.TempDir())
	require.NoError(b, ordermodels.StartOrderWriteStore())
	b.Cleanup(func() { require.NoError(b, ordermodels.StopOrderWriteStore()) })
	writer := orderbusiness.LocalOrderWriter{}
	commands := make07OrderCommands("bench-07-drain", newBenchmarkIDFactory(22), 120000000, b.N)
	for index := range commands {
		_, err := writer.Accept(context.Background(), commands[index])
		require.NoError(b, err)
	}
	syncer := orderbusiness.RemoteOrderSyncer{}
	b.ReportAllocs()
	b.ResetTimer()
	startedAt := time.Now()
	if err := syncer.DrainOnce(context.Background(), b.N); err != nil {
		b.Fatal(err)
	}
	elapsed := time.Since(startedAt)
	b.StopTimer()
	if elapsed > 0 {
		b.ReportMetric(float64(b.N)/elapsed.Seconds(), "orders/s")
	}
}

func make07OrderCommands(prefix string, ids benchmarkIDFactory, userBase uint, count int) []orderbusiness.CreateOrderCommand {
	commands := make([]orderbusiness.CreateOrderCommand, count)
	suffix := time.Now().UnixNano()
	for index := range commands {
		requestID := fmt.Sprintf("%s-%d-%d", prefix, suffix, index)
		commands[index] = orderbusiness.CreateOrderCommand{
			OrderID:            ids.NewID(),
			UserID:             userBase + uint(index),
			RequestID:          requestID,
			RequestFingerprint: requestID,
			SupplierID:         220001,
			ProductID:          320001,
			UnitPrice:          decimal.NewFromInt(10),
			Quantity:           2,
			TraceID:            "bench-trace-" + requestID,
			ServiceName:        "shop-order",
			ServiceInstanceID:  "bench-order",
		}
	}
	return commands
}
