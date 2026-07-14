package private

import (
	"hash/fnv"
	"strings"

	"github.com/digitalwayhk/core/examples/01-simple-shop/api/responsemodel"
	"github.com/digitalwayhk/core/examples/01-simple-shop/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// OrderEvent 是订单新增或删除后发送给当前用户的通知。
type OrderEvent struct {
	Action string               `json:"action"`
	Order  *responsemodel.Order `json:"order"`
}

// notifyOrderChange 通过当前 ServiceContext 中已冻结的订单路由发布用户通知。
func notifyOrderChange(req servertypes.IRequest, event *OrderEvent) {
	serviceContext := router.GetContext(req.ServiceName())
	if serviceContext == nil || serviceContext.Router == nil {
		return
	}
	info := serviceContext.Router.GetRouter("/api/shop/getorders")
	if info != nil {
		info.NoticeWebSocket(event)
	}
}

// GetOrders 查询当前登录用户的订单，并作为该用户的 WebSocket 订阅路由。
type GetOrders struct {
	UserID   string `json:"userID"`
	UserName string `json:"-"`
}

// Parse 不接受客户端身份参数，身份只能来自认证上下文或 WebSocket 登录会话。
func (own *GetOrders) Parse(servertypes.IRequest) error {
	return nil
}

// Validation 从 HTTP 认证上下文补齐用户身份并拒绝匿名访问。
func (own *GetOrders) Validation(req servertypes.IRequest) error {
	if own.UserID == "" {
		own.UserID, own.UserName = req.GetUser()
	}
	if strings.TrimSpace(own.UserID) == "" {
		return models.NewBusinessError("用户身份无效")
	}
	return nil
}

// Do 只按当前 UserID 查询订单，绝不接受调用方指定其他用户。
func (own *GetOrders) Do(servertypes.IRequest) (interface{}, error) {
	orders, err := models.NewOrder().QueryByUser(own.UserID)
	if err != nil {
		return nil, err
	}
	return responsemodel.Orders(orders), nil
}

// GetResponse 返回 OpenAPI 用的本人订单列表成功响应结构。
func (own *GetOrders) GetResponse() interface{} {
	return []*responsemodel.Order{}
}

// RouterInfo 将本人订单查询注册为需要认证的 GET 路由。
func (own *GetOrders) RouterInfo() *servertypes.RouterInfo {
	info := router.DefaultRouterInfo(own)
	info.Method = "GET"
	return info
}

// SetUserID 接收 WebSocket logon 会话解析出的可信用户身份。
func (own *GetOrders) SetUserID(userID, userName string) {
	own.UserID = userID
	own.UserName = userName
}

// GetUserID 返回当前订阅绑定的可信用户 ID。
func (own *GetOrders) GetUserID() string {
	return own.UserID
}

// GetHashKey 以 UserID 生成稳定订阅哈希，实现不同用户的订阅分组。
func (own *GetOrders) GetHashKey() uint64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(own.UserID))
	return hash.Sum64()
}

// NoticeFiltersRouter 只允许订单事件投递给同一用户的订阅实例。
func (own *GetOrders) NoticeFiltersRouter(message interface{}, api servertypes.IRouter) (bool, interface{}) {
	event, ok := message.(*OrderEvent)
	if !ok || event == nil || event.Order == nil {
		return false, nil
	}
	subscription, ok := api.(*GetOrders)
	if !ok || subscription.UserID == "" {
		return false, nil
	}
	return event.Order.UserID == subscription.UserID, event
}
