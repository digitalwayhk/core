package public

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/business"
	supplierapi "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/api/public"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

type CreateOrder struct {
	UserID    uint                    `json:"userID"`
	ProductID uint                    `json:"productID"`
	Quantity  int                     `json:"quantity"`
	RequestID string                  `json:"requestID"`
	Address   userdto.AddressSnapshot `json:"address"`
}

func (own *CreateOrder) Parse(req servertypes.IRequest) error { return req.Bind(own) }
func (own *CreateOrder) Validation(servertypes.IRequest) error {
	if own.UserID == 0 || own.ProductID == 0 || own.Quantity <= 0 || strings.TrimSpace(own.RequestID) == "" || own.Address.AddressID == 0 {
		return errors.New("下单参数不完整")
	}
	return nil
}
func (own *CreateOrder) Do(req servertypes.IRequest) (interface{}, error) {
	response, err := req.CallService(&supplierapi.GetProducts{ID: own.ProductID})
	if err != nil {
		return nil, err
	}
	if !response.GetSuccess() {
		return nil, response.GetError()
	}
	products := []*supplierdto.Product{}
	response.GetData(&products)
	if len(products) != 1 || products[0] == nil || !products[0].Enabled {
		return nil, contract.ErrResourceNotFound
	}
	product := products[0]
	snapshot := supplierdto.ProductSnapshot{ProductID: product.ID, SupplierID: product.SupplierID, SupplierCode: product.SupplierCode, SupplierName: product.SupplierName, ProductCode: product.Code, ProductName: product.Name, UnitPrice: product.Price}
	return business.CreateOrder(business.CreateOrderCommand{OrderID: req.NewID(), UserID: own.UserID, RequestID: own.RequestID, EventID: strconv.FormatUint(uint64(req.NewID()), 10), ProductID: own.ProductID, Quantity: own.Quantity, Address: own.Address}, snapshot)
}
func (*CreateOrder) GetResponse() interface{} { return &orderdto.Order{} }
func (own *CreateOrder) RouterInfo() *servertypes.RouterInfo {
	return orderPublicRoute(own, "createorder", http.MethodPost)
}

type CancelOrder struct {
	UserID  uint `json:"userID"`
	OrderID uint `json:"orderID"`
}

func (own *CancelOrder) Parse(req servertypes.IRequest) error { return req.Bind(own) }
func (own *CancelOrder) Validation(servertypes.IRequest) error {
	if own.UserID == 0 || own.OrderID == 0 {
		return errors.New("用户和订单不能为空")
	}
	return nil
}
func (own *CancelOrder) Do(req servertypes.IRequest) (interface{}, error) {
	return business.CancelOrder(own.UserID, own.OrderID, strconv.FormatUint(uint64(req.NewID()), 10))
}
func (*CancelOrder) GetResponse() interface{} { return &orderdto.Order{} }
func (own *CancelOrder) RouterInfo() *servertypes.RouterInfo {
	return orderPublicRoute(own, "cancelorder", http.MethodPost)
}

type CreatePayment struct {
	UserID        uint `json:"userID"`
	OrderID       uint `json:"orderID"`
	PaymentTypeID uint `json:"paymentTypeID"`
}

func (own *CreatePayment) Parse(req servertypes.IRequest) error { return req.Bind(own) }
func (own *CreatePayment) Validation(servertypes.IRequest) error {
	if own.UserID == 0 || own.OrderID == 0 || own.PaymentTypeID == 0 {
		return errors.New("支付参数不完整")
	}
	return nil
}
func (own *CreatePayment) Do(req servertypes.IRequest) (interface{}, error) {
	return business.CreatePayment(own.UserID, own.OrderID, own.PaymentTypeID, strconv.FormatUint(uint64(req.NewID()), 10), strconv.FormatUint(uint64(req.NewID()), 10))
}
func (*CreatePayment) GetResponse() interface{} { return &orderdto.PaymentRecord{} }
func (own *CreatePayment) RouterInfo() *servertypes.RouterInfo {
	return orderPublicRoute(own, "createpayment", http.MethodPost)
}

type GetOrders struct {
	UserID uint `json:"userID"`
}

func (own *GetOrders) Parse(req servertypes.IRequest) error { return req.Bind(own) }
func (own *GetOrders) Validation(servertypes.IRequest) error {
	if own.UserID == 0 {
		return errors.New("用户不能为空")
	}
	return nil
}
func (own *GetOrders) Do(servertypes.IRequest) (interface{}, error) {
	return business.UserOrders(own.UserID)
}
func (*GetOrders) GetResponse() interface{} { return []*orderdto.Order{} }
func (own *GetOrders) RouterInfo() *servertypes.RouterInfo {
	return orderPublicRoute(own, "getorders", http.MethodPost)
}

func orderPublicRoute(api interface{}, name, method string) *servertypes.RouterInfo {
	return router.DefaultRouterInfoWithOptions(api,
		router.WithServiceName(contract.OrderServiceName),
		router.WithPath("/api/"+contract.OrderServiceName+"/"+name),
		router.WithPathType(servertypes.PublicType),
		router.WithAuth(false),
		router.WithMethod(method),
		router.WithInternalCallers(contract.UserServiceName),
	)
}
