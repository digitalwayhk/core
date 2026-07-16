package private

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	orderapi "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/private"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

// AddOrder 在 User Service 验证本人地址后调用 Order Service。
type AddOrder struct {
	ProductID uint `json:"productID"`
	Quantity  int  `json:"quantity"`
	AddressID uint `json:"addressID"`
}

func (a *AddOrder) Parse(req servertypes.IRequest) error { return req.Bind(a) }
func (a *AddOrder) Validation(req servertypes.IRequest) error {
	if a.ProductID == 0 || a.Quantity <= 0 || a.AddressID == 0 {
		return errors.New("商品、数量和地址不能为空")
	}
	_, err := trustedUser(req)
	return err
}
func (a *AddOrder) Do(req servertypes.IRequest) (interface{}, error) {
	uid, _ := trustedUser(req)
	address, err := models.FindOwnedAddress(uid, a.AddressID)
	if err != nil || address == nil {
		return nil, errors.New("地址不存在或无权使用")
	}
	key := uid + "-" + strconv.FormatUint(uint64(req.NewID()), 10)
	res, err := req.CallService(&orderapi.CreateOrder{ProductID: a.ProductID, Quantity: a.Quantity, IdempotencyKey: key, Address: models.AddressSnapshot(address)})
	if err != nil {
		return nil, err
	}
	if !res.GetSuccess() {
		return nil, res.GetError()
	}
	result := &orderdto.Order{}
	res.GetData(result)
	return result, nil
}
func (*AddOrder) GetResponse() interface{}              { return &orderdto.Order{} }
func (a *AddOrder) RouterInfo() *servertypes.RouterInfo { return router.DefaultRouterInfo(a) }

// GetOrders 查询本人订单，并作为买家 WebSocket 订阅路由。
type GetOrders struct{ subscriptionUserID string }

func (*GetOrders) Parse(servertypes.IRequest) error { return nil }
func (g *GetOrders) Validation(req servertypes.IRequest) error {
	if g.resolveUserID(req) == "" {
		return errors.New("用户身份无效")
	}
	return nil
}
func (g *GetOrders) Do(req servertypes.IRequest) (interface{}, error) {
	res, err := req.CallService(&orderapi.GetUserOrders{})
	if err != nil {
		return nil, err
	}
	if !res.GetSuccess() {
		return nil, res.GetError()
	}
	items := []*orderdto.Order{}
	res.GetData(&items)
	return items, nil
}
func (*GetOrders) GetResponse() interface{} { return []*orderdto.Order{} }
func (g *GetOrders) RouterInfo() *servertypes.RouterInfo {
	return router.DefaultRouterInfoWithOptions(g, router.WithMethod(http.MethodGet))
}
func (g *GetOrders) SetUserID(uid, _ string) { g.subscriptionUserID = strings.TrimSpace(uid) }
func (g *GetOrders) GetUserID() string       { return g.subscriptionUserID }
func (g *GetOrders) GetHashKey() uint64      { return utils.HashCode64(g.subscriptionUserID) }
func (g *GetOrders) NoticeFiltersRouter(message interface{}, api servertypes.IRouter) (bool, interface{}) {
	event, ok := message.(*orderdto.Order)
	if !ok || event == nil {
		return false, nil
	}
	subscription, ok := api.(*GetOrders)
	if !ok || subscription.subscriptionUserID == "" || event.UserID != subscription.subscriptionUserID {
		return false, nil
	}
	return true, event
}
func (g *GetOrders) resolveUserID(req servertypes.IRequest) string {
	if req != nil {
		uid, _ := req.GetUser()
		if strings.TrimSpace(uid) != "" {
			return strings.TrimSpace(uid)
		}
	}
	return strings.TrimSpace(g.subscriptionUserID)
}

type DeleteOrder struct {
	OrderID uint `json:"orderID"`
}

func (d *DeleteOrder) Parse(req servertypes.IRequest) error { return req.Bind(d) }
func (d *DeleteOrder) Validation(req servertypes.IRequest) error {
	if d.OrderID == 0 {
		return errors.New("订单 ID 不能为空")
	}
	_, err := trustedUser(req)
	return err
}
func (d *DeleteOrder) Do(req servertypes.IRequest) (interface{}, error) {
	res, err := req.CallService(&orderapi.DeleteOrder{OrderID: d.OrderID})
	if err != nil {
		return nil, err
	}
	if !res.GetSuccess() {
		return nil, res.GetError()
	}
	item := &orderdto.Order{}
	res.GetData(item)
	return item, nil
}
func (*DeleteOrder) GetResponse() interface{}              { return &orderdto.Order{} }
func (d *DeleteOrder) RouterInfo() *servertypes.RouterInfo { return router.DefaultRouterInfo(d) }
