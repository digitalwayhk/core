// Package shoporderscale 提供 07 订单水平扩展示例的写入基准测试。
// 基准聚焦本地可靠 pending 接收与远程同步分离后的热路径，便于和 06 同步下单路径做趋势对比。
package shoporderscale

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	orderbusiness "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/business"
	ordermodels "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// Benchmark07LocalOrderAccept 测量 07 单个副本只写本地 pending 的饱和曲线。
func Benchmark07LocalOrderAccept(b *testing.B) {
	for _, concurrency := range benchmarkConcurrencies() {
		concurrency := concurrency
		b.Run(fmt.Sprintf("concurrency-%d", concurrency), func(b *testing.B) {
			runtime := newIntegrationOrderRuntime(b, nil)
			commands := make07OrderCommands("bench-07-local", newBenchmarkIDFactory(21), 100000000, b.N)
			writer := orderbusiness.LocalOrderWriter{Store: runtime}
			runBusinessBenchmarkSingleConcurrency(b, concurrency, "orders/s", func(index int) error {
				_, err := writer.Accept(context.Background(), commands[index])
				return err
			})
		})
	}
}

// Benchmark07HorizontalOrderAccept 测量多个独立 order-service 副本的聚合本地接单能力。
// 每个副本拥有不同 MachineID 和 Badger 路径；concurrency-per-replica 表示单副本客户端并发，
// 实际总并发等于副本数乘以单副本并发，请求按索引轮询路由到各个副本。
func Benchmark07HorizontalOrderAccept(b *testing.B) {
	for _, replicas := range benchmarkReplicaCounts() {
		replicas := replicas
		for _, concurrencyPerReplica := range benchmarkConcurrencies() {
			concurrencyPerReplica := concurrencyPerReplica
			b.Run(fmt.Sprintf("replicas-%d/concurrency-per-replica-%d", replicas, concurrencyPerReplica), func(b *testing.B) {
				basePath := b.TempDir()
				writers := make([]orderbusiness.LocalOrderWriter, replicas)
				idFactories := make([]benchmarkIDFactory, replicas)
				for replica := 0; replica < replicas; replica++ {
					machineID := int64(replica + 1)
					runtime := newIntegrationOrderReplicaRuntime(b, nil, basePath, machineID)
					writers[replica] = orderbusiness.LocalOrderWriter{Store: runtime}
					idFactories[replica] = newBenchmarkIDFactory(uint(machineID))
				}

				commands := make07OrderCommands("bench-07-horizontal", idFactories[0], 110000000, b.N)
				for index := range commands {
					replica := index % replicas
					if replica > 0 {
						commands[index].OrderID = idFactories[replica].NewID()
					}
					commands[index].ServiceInstanceID = fmt.Sprintf("bench-order-%d", replica+1)
				}

				totalConcurrency := replicas * concurrencyPerReplica
				elapsed := runBusinessBenchmarkSingleConcurrency(b, totalConcurrency, "aggregate-orders/s", func(index int) error {
					replica := index % replicas
					_, err := writers[replica].Accept(context.Background(), commands[index])
					return err
				})
				if elapsed > 0 {
					perReplica := float64(b.N) / elapsed.Seconds() / float64(replicas)
					b.ReportMetric(perReplica, "orders/replica/s")
				}
			})
		}
	}
}

// Benchmark07DrainPendingOnce 测量 07 从本地 pending 同步到远程权威库的批量成本。
func Benchmark07DrainPendingOnce(b *testing.B) {
	requireOrderMySQL(b)
	runtime := newIntegrationOrderRuntime(b, nil)
	writer := orderbusiness.LocalOrderWriter{Store: runtime}
	commands := make07OrderCommands("bench-07-drain", newBenchmarkIDFactory(22), 120000000, b.N)
	cleanup07DrainBenchmarkData(b, commands)
	for index := range commands {
		_, err := writer.Accept(context.Background(), commands[index])
		require.NoError(b, err)
	}
	syncer := orderbusiness.RemoteOrderSyncer{Store: runtime}
	b.ReportAllocs()
	b.ResetTimer()
	startedAt := time.Now()
	result, err := syncer.DrainOnce(context.Background(), b.N)
	if err != nil {
		b.Fatal(err)
	}
	elapsed := time.Since(startedAt)
	b.StopTimer()
	if elapsed > 0 {
		b.ReportMetric(float64(result.Confirmed)/elapsed.Seconds(), "orders/s")
		if result.Confirmed > 0 {
			b.ReportMetric(float64(elapsed.Nanoseconds())/float64(result.Confirmed), "ns/confirmed-order")
		}
	}
	b.ReportMetric(float64(result.Confirmed), "confirmed-orders")
}

// cleanup07DrainBenchmarkData 在 benchmark 结束后删除本轮生成的远程订单与 Outbox。
// 清理位于计时区间外，避免历史未发布事件阻塞后续 WebSocket UAT。
func cleanup07DrainBenchmarkData(b *testing.B, commands []orderbusiness.CreateOrderCommand) {
	b.Helper()
	orderHashes := make([]string, 0, len(commands))
	outboxHashes := make([]string, 0, len(commands))
	for _, command := range commands {
		order := ordermodels.NewOrder()
		order.UserID = command.UserID
		order.RequestID = command.RequestID
		orderHashes = append(orderHashes, order.GetHash())

		outbox := ordermodels.NewOutbox()
		outbox.EventID = fmt.Sprintf("order:%d:%s", command.OrderID, contract.EventOrderCreated)
		outboxHashes = append(outboxHashes, outbox.GetHash())
	}
	b.Cleanup(func() {
		err := ordermodels.RunRemoteTransaction(func(action ordermodels.DataAction) error {
			var outboxes []*ordermodels.OutboxRecord
			outboxQuery := &persistencetypes.SearchItem{Page: 1, Size: len(outboxHashes), Model: ordermodels.NewOutbox()}
			outboxQuery.AddWhereNS("Hashcode", persistencetypes.SymbolIn, outboxHashes)
			if err := action.Load(outboxQuery, &outboxes); err != nil {
				return err
			}
			if len(outboxes) > 0 {
				if err := action.Delete(outboxes); err != nil {
					return err
				}
			}

			var orders []*ordermodels.Order
			orderQuery := &persistencetypes.SearchItem{Page: 1, Size: len(orderHashes), Model: ordermodels.NewOrder()}
			orderQuery.AddWhereNS("Hashcode", persistencetypes.SymbolIn, orderHashes)
			if err := action.Load(orderQuery, &orders); err != nil {
				return err
			}
			if len(orders) > 0 {
				return action.Delete(orders)
			}
			return nil
		})
		require.NoError(b, err)
	})
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
