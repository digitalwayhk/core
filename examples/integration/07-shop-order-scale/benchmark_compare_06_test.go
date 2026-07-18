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
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		requestID := fmt.Sprintf("bench-06-sync-%d-%d", time.Now().UnixNano(), index)
		orderID := uint(time.Now().UnixNano()%1_000_000_000) + uint(index) + 860000000
		_, err := business06.CreateOrder(business06.CreateOrderCommand{
			OrderID:   orderID,
			UserID:    uint(160000000 + index),
			RequestID: requestID,
			TraceID:   "bench-trace-" + requestID,
			EventID:   "event-" + requestID,
			ProductID: product.ProductID,
			Quantity:  2,
			Address:   address,
		}, product)
		if err != nil {
			b.Fatal(err)
		}
	}
}
