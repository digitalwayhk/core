// seed-report-orders 向 07 远程 MySQL 权威库写入跨时间、跨商品/供应商的样例订单，便于报表演示。
//
// 用法（仓库根或本目录，需已启动 MySQL）：
//
//	export SHOP_ORDER_REMOTE_MYSQL_PASSWORD=shop-root
//	go run ./examples/07-shop-order-scale/tools/seed-report-orders
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/transaction"
	"github.com/shopspring/decimal"
)

type seedRow struct {
	dayOffset  int // 相对今天的天数，0=今天，-1=昨天
	userID     uint
	supplierID uint
	productID  uint
	supCode    string
	supName    string
	prodCode   string
	prodName   string
	unitPrice  string
	qty        int
	paid       bool
}

func main() {
	if os.Getenv("SHOP_ORDER_REMOTE_MYSQL_PASSWORD") == "" {
		_ = os.Setenv("SHOP_ORDER_REMOTE_MYSQL_PASSWORD", "shop-root")
	}
	if os.Getenv("SHOP_ORDER_REMOTE_MYSQL_DATABASE") == "" {
		_ = os.Setenv("SHOP_ORDER_REMOTE_MYSQL_DATABASE", "shop_order_scale_remote")
	}
	if os.Getenv("SHOP_ORDER_REMOTE_MYSQL_HOST") == "" {
		_ = os.Setenv("SHOP_ORDER_REMOTE_MYSQL_HOST", "127.0.0.1")
	}

	if err := models.EnsureStorage(); err != nil {
		panic(err)
	}

	// 覆盖约 14 天 + 2 个月，多商品/供应商，金额有落差，方便看趋势/占比/月报
	rows := []seedRow{
		// --- 近两周按日 ---
		{-13, 1001, 1, 101, "SUP-A", "华南供应商", "SKU-PHONE", "智能手机 Pro", "3999.00", 2, true},
		{-13, 1002, 1, 102, "SUP-A", "华南供应商", "SKU-CASE", "保护壳", "59.00", 5, true},
		{-12, 1003, 2, 201, "SUP-B", "华东供应商", "SKU-EAR", "无线耳机", "899.00", 3, true},
		{-11, 1001, 1, 101, "SUP-A", "华南供应商", "SKU-PHONE", "智能手机 Pro", "3999.00", 1, true},
		{-10, 1004, 2, 202, "SUP-B", "华东供应商", "SKU-PAD", "平板 11 寸", "2999.00", 1, true},
		{-9, 1002, 3, 301, "SUP-C", "华北供应商", "SKU-WATCH", "智能手表", "1299.00", 2, true},
		{-8, 1005, 1, 102, "SUP-A", "华南供应商", "SKU-CASE", "保护壳", "59.00", 20, true},
		{-7, 1001, 2, 201, "SUP-B", "华东供应商", "SKU-EAR", "无线耳机", "899.00", 4, true},
		{-6, 1003, 1, 101, "SUP-A", "华南供应商", "SKU-PHONE", "智能手机 Pro", "3999.00", 3, true},
		{-5, 1006, 3, 302, "SUP-C", "华北供应商", "SKU-BAND", "运动手环", "199.00", 8, true},
		{-4, 1002, 2, 202, "SUP-B", "华东供应商", "SKU-PAD", "平板 11 寸", "2999.00", 2, true},
		{-3, 1004, 1, 103, "SUP-A", "华南供应商", "SKU-CHARGER", "快充头 65W", "129.00", 10, true},
		{-2, 1001, 3, 301, "SUP-C", "华北供应商", "SKU-WATCH", "智能手表", "1299.00", 1, true},
		{-1, 1005, 1, 101, "SUP-A", "华南供应商", "SKU-PHONE", "智能手机 Pro", "3999.00", 2, true},
		{-1, 1003, 2, 201, "SUP-B", "华东供应商", "SKU-EAR", "无线耳机", "899.00", 6, true},
		{0, 1007, 1, 102, "SUP-A", "华南供应商", "SKU-CASE", "保护壳", "59.00", 15, true},
		{0, 1002, 3, 302, "SUP-C", "华北供应商", "SKU-BAND", "运动手环", "199.00", 4, false},
		// --- 上月（用于供应商月报）---
		{-32, 1008, 1, 101, "SUP-A", "华南供应商", "SKU-PHONE", "智能手机 Pro", "3999.00", 5, true},
		{-35, 1009, 2, 202, "SUP-B", "华东供应商", "SKU-PAD", "平板 11 寸", "2999.00", 3, true},
		{-40, 1010, 3, 301, "SUP-C", "华北供应商", "SKU-WATCH", "智能手表", "1299.00", 4, true},
		// --- 再上月 ---
		{-65, 1011, 1, 103, "SUP-A", "华南供应商", "SKU-CHARGER", "快充头 65W", "129.00", 30, true},
		{-70, 1012, 2, 201, "SUP-B", "华东供应商", "SKU-EAR", "无线耳机", "899.00", 8, true},
	}

	now := time.Now().UTC()
	// 固定时分，避免同日 bucket 抖动
	baseClock := time.Date(now.Year(), now.Month(), now.Day(), 10, 30, 0, 0, time.UTC)

	orders := make([]*transaction.Order, 0, len(rows))
	for i, r := range rows {
		created := baseClock.AddDate(0, 0, r.dayOffset).Add(time.Duration(i) * time.Minute)
		price := decimal.RequireFromString(r.unitPrice)
		total := price.Mul(decimal.NewFromInt(int64(r.qty)))
		o := models.NewOrder()
		o.SetCreatedAt(created)
		o.SetUpdatedAt(created)
		o.AcceptedAt = created
		synced := created.Add(2 * time.Minute)
		o.SyncedAt = &synced
		o.RequestID = fmt.Sprintf("seed-report-%04d-%s", i+1, created.Format("20060102"))
		o.RequestFingerprint = fmt.Sprintf("fp-%s", o.RequestID)
		o.UserID = r.userID
		o.SupplierID = r.supplierID
		o.ProductID = r.productID
		o.SupplierCode = r.supCode
		o.SupplierName = r.supName
		o.ProductCode = r.prodCode
		o.ProductName = r.prodName
		o.UnitPrice = price
		o.Quantity = r.qty
		o.TotalAmount = total
		o.Recipient = "演示用户"
		o.Phone = "13800000000"
		o.Region = "广东省/深圳市"
		o.AddressDetail = "科技园演示地址"
		o.AddressID = 1
		o.OrderStatus = models.OrderStatusSynced
		if r.paid {
			o.PaymentStatus = models.PaymentStatusPaid
			o.CurrentPaymentID = fmt.Sprintf("pay-%s", o.RequestID)
		} else {
			o.PaymentStatus = models.PaymentStatusUnpaid
		}
		o.TraceID = fmt.Sprintf("trace-seed-%04d", i+1)
		o.ServiceName = "shop-order"
		o.ServiceInstanceID = "seed-tool"
		o.ServiceInstanceIP = "127.0.0.1"
		orders = append(orders, o)
	}

	err := models.RunRemoteTransaction(func(action models.DataAction) error {
		_, err := models.UpsertRemoteOrdersWith(action, orders)
		return err
	})
	if err != nil {
		panic(err)
	}

	// 汇总打印
	list, _, err := models.ListRemoteOrdersWith(models.RemoteDataAction(), models.OrderQueryFilter{}, 1, 500)
	if err != nil {
		panic(err)
	}
	fmt.Printf("seed ok: wrote %d orders (remote total rows now ~%d)\n", len(orders), len(list))
	byDay := map[string]int{}
	for _, o := range list {
		if o.GetCreatedAt() == nil {
			continue
		}
		d := o.GetCreatedAt().UTC().Format("2006-01-02")
		byDay[d]++
	}
	fmt.Println("orders by day (sample of current list page):")
	for d, n := range byDay {
		fmt.Printf("  %s: %d\n", d, n)
	}
	fmt.Println("next: open Admin 报表 and click 刷新数据 (or POST analysis/reports with refresh:true)")
}
