// Package public 提供 07 订单服务下单内部 Public API。
package public

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	orderdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/order"
	userdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/user"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/business"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/shopspring/decimal"
)

// CreateOrder 是 user-service 调用 order-service 的可靠下单入口。
type CreateOrder struct {
	UserID             uint                    `json:"userID"`
	SupplierID         uint                    `json:"supplierID"`
	ProductID          uint                    `json:"productID"`
	Quantity           int                     `json:"quantity"`
	RequestID          string                  `json:"requestID"`
	RequestFingerprint string                  `json:"requestFingerprint"`
	SupplierCode       string                  `json:"supplierCode"`
	SupplierName       string                  `json:"supplierName"`
	ProductCode        string                  `json:"productCode"`
	ProductName        string                  `json:"productName"`
	UnitPrice          decimal.Decimal         `json:"unitPrice"`
	Address            userdto.AddressSnapshot `json:"address"`
}

// Parse 绑定下单请求。
func (own *CreateOrder) Parse(req servertypes.IRequest) error { return req.Bind(own) }

// Validation 校验下单请求的内部可信参数。
func (own *CreateOrder) Validation(servertypes.IRequest) error {
	if own.UserID == 0 || own.SupplierID == 0 || own.ProductID == 0 || own.Quantity <= 0 || strings.TrimSpace(own.RequestID) == "" || !own.UnitPrice.IsPositive() {
		return errors.New("下单参数不完整")
	}
	return nil
}

// Do 将订单请求写入本地 pending 并返回 accepted 快照。
func (own *CreateOrder) Do(req servertypes.IRequest) (interface{}, error) {
	fingerprint := strings.TrimSpace(own.RequestFingerprint)
	if fingerprint == "" {
		fingerprint = own.RequestID
	}
	serviceInstanceID, serviceInstanceIP := orderRuntimeIdentity()
	command := business.CreateOrderCommand{
		OrderID:            req.NewID(),
		UserID:             own.UserID,
		RequestID:          own.RequestID,
		RequestFingerprint: fingerprint,
		SupplierID:         own.SupplierID,
		ProductID:          own.ProductID,
		SupplierCode:       own.SupplierCode,
		SupplierName:       own.SupplierName,
		ProductCode:        own.ProductCode,
		ProductName:        own.ProductName,
		UnitPrice:          own.UnitPrice,
		Quantity:           own.Quantity,
		Recipient:          own.Address.ReceiverName,
		Phone:              own.Address.Phone,
		Region:             own.Address.Province + own.Address.City + own.Address.District,
		AddressDetail:      own.Address.Detail,
		AddressID:          own.Address.AddressID,
		TraceID:            req.GetTraceId(),
		ServiceName:        req.ServiceName(),
		ServiceInstanceID:  serviceInstanceID,
		ServiceInstanceIP:  serviceInstanceIP,
	}
	orderID, err := (business.LocalOrderWriter{}).Accept(context.Background(), command)
	if err != nil {
		return nil, err
	}
	return &orderdto.Order{
		OrderID:       orderID,
		UserID:        own.UserID,
		SupplierID:    own.SupplierID,
		ProductID:     own.ProductID,
		Quantity:      own.Quantity,
		UnitPrice:     own.UnitPrice,
		TotalAmount:   own.UnitPrice.Mul(decimal.NewFromInt(int64(own.Quantity))),
		OrderStatus:   "accepted",
		PaymentStatus: "unpaid",
		TraceID:       req.GetTraceId(),
	}, nil
}

func orderRuntimeIdentity() (string, string) {
	sc := router.GetContext(contract.OrderServiceName)
	if sc == nil {
		return "", ""
	}
	address := ""
	if sc.Config != nil && sc.Config.Cluster.AdvertiseAddress != "" {
		address = sc.Config.Cluster.AdvertiseAddress
	}
	return sc.ServiceInstanceID, address
}

// GetResponse 返回下单响应 DTO 类型。
func (*CreateOrder) GetResponse() interface{} { return &orderdto.Order{} }

// RouterInfo 返回下单内部 Public 路由信息。
func (own *CreateOrder) RouterInfo() *servertypes.RouterInfo {
	return orderPublicRoute(own, "createorder", http.MethodPost)
}
