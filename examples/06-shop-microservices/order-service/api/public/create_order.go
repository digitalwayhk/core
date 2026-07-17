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
