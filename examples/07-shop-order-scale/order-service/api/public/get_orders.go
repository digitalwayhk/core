// Package public 提供 07 订单服务统一远程权威订单查询 API。
package public

import (
	"errors"
	"net/http"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	orderdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/order"
	userdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/user"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/business"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// GetOrders 是 user/supplier 服务查询订单的内部 Public API。
type GetOrders struct {
	UserID     uint `json:"userID"`
	SupplierID uint `json:"supplierID"`
	Page       int  `json:"page"`
	Size       int  `json:"size"`
}

// Parse 绑定订单查询请求。
func (own *GetOrders) Parse(req servertypes.IRequest) error { return req.Bind(own) }

// Validation 校验内部查询必须指定买家或供应商限域。
func (own *GetOrders) Validation(servertypes.IRequest) error {
	if own.UserID == 0 && own.SupplierID == 0 {
		return errors.New("订单查询缺少限域")
	}
	return nil
}

// Do 从共享远程权威库查询订单。
func (own *GetOrders) Do(servertypes.IRequest) (interface{}, error) {
	items, _, err := business.ListOrders(models.OrderQueryFilter{UserID: own.UserID, SupplierID: own.SupplierID}, own.Page, own.Size)
	if err != nil {
		return nil, err
	}
	result := make([]*orderdto.Order, 0, len(items))
	for _, item := range items {
		result = append(result, orderToDTO(item))
	}
	return result, nil
}

// GetResponse 返回订单列表响应 DTO 类型。
func (*GetOrders) GetResponse() interface{} { return []*orderdto.Order{} }

// RouterInfo 返回订单查询内部 Public 路由信息。
func (own *GetOrders) RouterInfo() *servertypes.RouterInfo {
	return orderPublicRoute(own, "getorders", http.MethodPost, contract.UserServiceName, contract.SupplierServiceName)
}

func orderToDTO(item *models.Order) *orderdto.Order {
	if item == nil {
		return nil
	}
	return &orderdto.Order{
		OrderID:          item.ID,
		OrderRevision:    item.OrderRevision,
		UserID:           item.UserID,
		SupplierID:       item.SupplierID,
		ProductID:        item.ProductID,
		SupplierCode:     item.SupplierCode,
		SupplierName:     item.SupplierName,
		ProductCode:      item.ProductCode,
		ProductName:      item.ProductName,
		UnitPrice:        item.UnitPrice,
		Quantity:         item.Quantity,
		TotalAmount:      item.TotalAmount,
		OrderStatus:      item.OrderStatus,
		PaymentStatus:    item.PaymentStatus,
		CurrentPaymentID: item.CurrentPaymentID,
		Address: userdto.AddressSnapshot{
			UserID:       item.UserID,
			AddressID:    item.AddressID,
			ReceiverName: item.Recipient,
			Phone:        item.Phone,
			Province:     item.Region,
			Detail:       item.AddressDetail,
			TraceID:      item.TraceID,
		},
		TraceID:           item.TraceID,
		ServiceName:       item.ServiceName,
		ServiceInstanceID: item.ServiceInstanceID,
		AcceptedAt:        item.AcceptedAt,
		SyncedAt:          item.SyncedAt,
	}
}
