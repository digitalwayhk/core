// 本文件提供 06 三进程 UAT 订单可见性断言使用的纯内存查找辅助能力。
// 这些 helper 不访问服务，只把各角色查询结果转换为明确的“是否可见”断言。
package shopmicroservices_test

import orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"

// findOrderByID 在订单 DTO 列表中按订单 ID 查找订单。
func findOrderByID(orders []*orderdto.Order, id uint) *orderdto.Order {
	for _, order := range orders {
		if order != nil && order.ID == id {
			return order
		}
	}
	return nil
}

// findSupplierOrderByID 在供应商订单投影列表中按订单 ID 查找订单。
func findSupplierOrderByID(orders []*orderdto.SupplierOrder, id uint) *orderdto.SupplierOrder {
	for _, order := range orders {
		if order != nil && order.OrderID == id {
			return order
		}
	}
	return nil
}
