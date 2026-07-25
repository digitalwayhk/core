// Package shoporderscale 提供 06 同步订单写入基准，作为 07 水平扩展写路径的对照组。
// 本文件只比较业务写入热路径趋势，不替代真实 HTTP 压测或生产容量评估。
package shoporderscale

import (
	"fmt"
	"testing"
	"time"

	supplierdto06 "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	userdto06 "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	business06 "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/business"
	"github.com/shopspring/decimal"
)

// Benchmark06CreateOrderDirect 测量 06 下单时直接写订单权威库和 Outbox 的同步成本。
func Benchmark06CreateOrderDirect(b *testing.B) {
	product := supplierdto06.ProductSnapshot{
		ProductID:    306001,
		SupplierID:   206001,
		SupplierCode: "bench-supplier-06",
		SupplierName: "06基准供应商",
		ProductCode:  "bench-product-06",
		ProductName:  "06基准商品",
		UnitPrice:    decimal.NewFromInt(10),
	}
	address := userdto06.AddressSnapshot{AddressID: 1, Recipient: "基准用户", Phone: "13800000000", Region: "广东深圳", Detail: "科技园"}
	for _, concurrency := range benchmarkConcurrencies() {
		concurrency := concurrency
		b.Run(fmt.Sprintf("concurrency-%d", concurrency), func(b *testing.B) {
			commands := make06OrderCommands(b.N, product.ProductID, address, newBenchmarkIDFactory(20))
			runBusinessBenchmarkSingleConcurrency(b, concurrency, "orders/s", func(index int) error {
				_, err := business06.CreateOrder(commands[index], product)
				return err
			})
		})
	}
}

func make06OrderCommands(count int, productID uint, address userdto06.AddressSnapshot, ids benchmarkIDFactory) []business06.CreateOrderCommand {
	commands := make([]business06.CreateOrderCommand, count)
	suffix := time.Now().UnixNano()
	for index := range commands {
		requestID := fmt.Sprintf("bench-06-sync-%d-%d", suffix, index)
		commands[index] = business06.CreateOrderCommand{
			OrderID:   ids.NewID(),
			UserID:    uint(160000000 + index),
			RequestID: requestID,
			TraceID:   "bench-trace-" + requestID,
			EventID:   "event-" + requestID,
			ProductID: productID,
			Quantity:  2,
			Address:   address,
		}
	}
	return commands
}
