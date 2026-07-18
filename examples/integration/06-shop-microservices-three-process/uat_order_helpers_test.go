package shopmicroservices_test

import orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"

func findOrderByID(orders []*orderdto.Order, id uint) *orderdto.Order {
	for _, order := range orders {
		if order != nil && order.ID == id {
			return order
		}
	}
	return nil
}

func findSupplierOrderByID(orders []*orderdto.SupplierOrder, id uint) *orderdto.SupplierOrder {
	for _, order := range orders {
		if order != nil && order.OrderID == id {
			return order
		}
	}
	return nil
}
