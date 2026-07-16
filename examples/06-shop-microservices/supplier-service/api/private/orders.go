package private

import (
	"errors"
	"net/http"
	"strings"
	"time"

	eventdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/event"
	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	orderapi "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/private"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

// GetOrders 查询当前供应商商品的订单，并作为供应商 WebSocket 订阅路由。
type GetOrders struct{ requestSupplierID, subscriptionSupplierID string }

func (*GetOrders) Parse(servertypes.IRequest) error { return nil }
func (g *GetOrders) Validation(req servertypes.IRequest) error {
	g.requestSupplierID = g.resolveSupplierID(req)
	if g.requestSupplierID == "" {
		return errors.New("供应商身份无效")
	}
	return nil
}
func (g *GetOrders) Do(req servertypes.IRequest) (interface{}, error) {
	response, err := req.CallService(&orderapi.GetSupplierOrders{})
	if err != nil {
		return nil, err
	}
	if !response.GetSuccess() {
		return nil, response.GetError()
	}
	items := []*orderdto.SupplierOrder{}
	response.GetData(&items)
	return items, nil
}
func (*GetOrders) GetResponse() interface{} { return []*orderdto.SupplierOrder{} }
func (g *GetOrders) RouterInfo() *servertypes.RouterInfo {
	info := router.DefaultRouterInfoWithOptions(g, router.WithMethod(http.MethodGet))
	info.UseCache(10 * time.Second)
	return info
}
func (g *GetOrders) GetCacheKey() string {
	uid := strings.TrimSpace(g.requestSupplierID)
	if uid == "" {
		uid = strings.TrimSpace(g.subscriptionSupplierID)
	}
	if uid == "" {
		return ""
	}
	return utils.HashCodes(uid)
}
func (g *GetOrders) Reset()                     { g.requestSupplierID = "" }
func (g *GetOrders) Clean()                     { g.requestSupplierID = "" }
func (g *GetOrders) SetUserID(userID, _ string) { g.subscriptionSupplierID = strings.TrimSpace(userID) }
func (g *GetOrders) GetUserID() string          { return g.subscriptionSupplierID }
func (g *GetOrders) GetHashKey() uint64         { return utils.HashCode64(g.subscriptionSupplierID) }
func (g *GetOrders) NoticeFiltersRouter(message interface{}, api servertypes.IRouter) (bool, interface{}) {
	change, ok := message.(*eventdto.OrderChanged)
	if !ok || change == nil {
		return false, nil
	}
	subscription, ok := api.(*GetOrders)
	if !ok || subscription.subscriptionSupplierID == "" || change.SupplierID != subscription.subscriptionSupplierID {
		return false, nil
	}
	return true, change
}
func (g *GetOrders) resolveSupplierID(req servertypes.IRequest) string {
	if req != nil {
		uid, _ := req.GetUser()
		if strings.TrimSpace(uid) != "" {
			return strings.TrimSpace(uid)
		}
	}
	return strings.TrimSpace(g.subscriptionSupplierID)
}

// NotifyOrderChanged 把已经 Inbox 幂等处理的事件投递给本节点外部订阅者。
func NotifyOrderChanged(change *eventdto.OrderChanged) error {
	if change == nil {
		return nil
	}
	(&GetOrders{}).RouterInfo().FailureCache(nil)
	(&GetOrders{}).RouterInfo().NoticeWebSocket(change)
	return nil
}
