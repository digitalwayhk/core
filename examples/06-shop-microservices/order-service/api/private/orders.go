package private

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/business"
	supplierapi "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/api/call"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

func identity(req servertypes.IRequest) (string, error) {
	uid, _ := req.GetUser()
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return "", errors.New("身份无效")
	}
	return uid, nil
}

// CreateOrder 是 User Service 的类型化下单目标 API。
type CreateOrder struct {
	ProductID      uint                    `json:"productID"`
	Quantity       int                     `json:"quantity"`
	IdempotencyKey string                  `json:"idempotencyKey"`
	Address        userdto.AddressSnapshot `json:"address"`
}

func (c *CreateOrder) Parse(req servertypes.IRequest) error { return req.Bind(c) }
func (c *CreateOrder) Validation(req servertypes.IRequest) error {
	if _, err := identity(req); err != nil {
		return err
	}
	if c.ProductID == 0 || c.Quantity <= 0 || strings.TrimSpace(c.IdempotencyKey) == "" || c.Address.AddressID == 0 {
		return errors.New("下单参数不完整")
	}
	return nil
}
func (c *CreateOrder) Do(req servertypes.IRequest) (interface{}, error) {
	res, err := req.CallService(&supplierapi.GetProductSnapshot{ProductID: c.ProductID})
	if err != nil {
		return nil, err
	}
	if !res.GetSuccess() {
		return nil, res.GetError()
	}
	snapshot := &supplierdto.ProductSnapshot{}
	res.GetData(snapshot)
	uid, _ := identity(req)
	return business.CreateOrder(req.NewID(), uid, c.IdempotencyKey, strconv.FormatUint(uint64(req.NewID()), 10), *snapshot, c.Address, c.Quantity)
}
func (*CreateOrder) GetResponse() interface{}              { return &orderdto.Order{} }
func (c *CreateOrder) RouterInfo() *servertypes.RouterInfo { return router.DefaultRouterInfo(c) }

type GetUserOrders struct{}

func (*GetUserOrders) Parse(servertypes.IRequest) error          { return nil }
func (*GetUserOrders) Validation(req servertypes.IRequest) error { _, err := identity(req); return err }
func (*GetUserOrders) Do(req servertypes.IRequest) (interface{}, error) {
	uid, _ := identity(req)
	return business.UserOrders(uid)
}
func (*GetUserOrders) GetResponse() interface{} { return []*orderdto.Order{} }
func (g *GetUserOrders) RouterInfo() *servertypes.RouterInfo {
	return router.DefaultRouterInfoWithOptions(g, router.WithMethod(http.MethodGet))
}

type GetSupplierOrders struct{}

func (*GetSupplierOrders) Parse(servertypes.IRequest) error { return nil }
func (*GetSupplierOrders) Validation(req servertypes.IRequest) error {
	_, err := identity(req)
	return err
}
func (*GetSupplierOrders) Do(req servertypes.IRequest) (interface{}, error) {
	uid, _ := identity(req)
	return business.SupplierOrders(uid)
}
func (*GetSupplierOrders) GetResponse() interface{} { return []*orderdto.SupplierOrder{} }
func (g *GetSupplierOrders) RouterInfo() *servertypes.RouterInfo {
	return router.DefaultRouterInfoWithOptions(g, router.WithMethod(http.MethodGet))
}

type DeleteOrder struct {
	OrderID uint `json:"orderID"`
}

func (d *DeleteOrder) Parse(req servertypes.IRequest) error { return req.Bind(d) }
func (d *DeleteOrder) Validation(req servertypes.IRequest) error {
	if d.OrderID == 0 {
		return errors.New("订单 ID 不能为空")
	}
	_, err := identity(req)
	return err
}
func (d *DeleteOrder) Do(req servertypes.IRequest) (interface{}, error) {
	uid, _ := identity(req)
	return business.DeleteOrCancel(uid, d.OrderID, strconv.FormatUint(uint64(req.NewID()), 10))
}
func (*DeleteOrder) GetResponse() interface{}              { return &orderdto.Order{} }
func (d *DeleteOrder) RouterInfo() *servertypes.RouterInfo { return router.DefaultRouterInfo(d) }
