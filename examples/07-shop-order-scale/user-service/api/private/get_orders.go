// Package private 提供 07 用户入口服务买家订单查询 API。
package private

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	orderdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/order"
	orderapi "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/public"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

// GetOrders 是买家查询本人订单入口。
type GetOrders struct {
	Page               int `json:"page"`
	Size               int `json:"size"`
	requestUserID      uint
	subscriptionUserID uint
	subscriptionAuthID string
}

// Parse 绑定订单查询请求。
func (own *GetOrders) Parse(req servertypes.IRequest) error { return req.Bind(own) }

// Validation 校验订单查询请求。
func (own *GetOrders) Validation(req servertypes.IRequest) error {
	userID, err := parseRequestUserID(req)
	if err != nil {
		return err
	}
	own.requestUserID = userID
	return nil
}

// Do 调用订单服务内部查询 API，只传 Token 映射出的数字 UserID。
func (own *GetOrders) Do(req servertypes.IRequest) (interface{}, error) {
	userID := own.requestUserID
	if userID == 0 {
		var err error
		userID, err = parseRequestUserID(req)
		if err != nil {
			return nil, err
		}
	}
	response, err := req.CallService(&orderapi.GetOrders{UserID: userID, Page: own.Page, Size: own.Size})
	if err != nil || !response.GetSuccess() {
		if err != nil {
			return nil, err
		}
		return nil, response.GetError()
	}
	var items []*orderdto.Order
	response.GetData(&items)
	return items, nil
}

// GetResponse 返回订单列表响应 DTO 类型。
func (*GetOrders) GetResponse() interface{} { return []*orderdto.Order{} }

// RouterInfo 返回买家订单查询 Private 路由信息。
func (own *GetOrders) RouterInfo() *servertypes.RouterInfo {
	info := userPrivateRoute(own, "getorders", http.MethodPost)
	info.UseCache(10 * time.Second)
	return info
}

// GetCacheKey 返回按买家数字身份隔离的订单查询缓存键。
func (own *GetOrders) GetCacheKey() string {
	userID := own.requestUserID
	if userID == 0 {
		userID = own.subscriptionUserID
	}
	if userID == 0 {
		return ""
	}
	return utils.HashCodes(strconv.FormatUint(uint64(userID), 10))
}

// InvalidateOrderCache 失效指定买家的订单查询入口缓存。
func InvalidateOrderCache(userID uint) {
	(&GetOrders{}).RouterInfo().FailureCache(&GetOrders{requestUserID: userID})
}

// Reset 清理路由对象池复用前保存的请求身份。
func (own *GetOrders) Reset() { own.requestUserID = 0 }

// Clean 清理路由对象池复用前保存的请求身份。
func (own *GetOrders) Clean() { own.requestUserID = 0 }

// SetUserID 保存 WebSocket 登录身份，用于后续订阅过滤。
func (own *GetOrders) SetUserID(uid, _ string) {
	own.subscriptionAuthID = strings.TrimSpace(uid)
	if parsed, err := strconv.ParseUint(own.subscriptionAuthID, 10, 64); err == nil {
		own.subscriptionUserID = uint(parsed)
	}
}

// GetUserID 返回当前 WebSocket 订阅绑定的登录身份。
func (own *GetOrders) GetUserID() string { return own.subscriptionAuthID }

// GetHashKey 返回 WebSocket 订阅过滤使用的买家数字身份。
func (own *GetOrders) GetHashKey() uint64 { return uint64(own.subscriptionUserID) }

// NoticeFiltersRouter 判断订单事件是否应该推送给当前订阅买家。
func (*GetOrders) NoticeFiltersRouter(message interface{}, api servertypes.IRouter) (bool, interface{}) {
	event, ok := message.(*orderdto.OrderChanged)
	if !ok || event == nil {
		return false, nil
	}
	subscription, ok := api.(*GetOrders)
	if !ok || subscription.subscriptionUserID == 0 || event.UserID != subscription.subscriptionUserID {
		return false, nil
	}
	return true, event
}

// parseRequestUserID 从可信认证上下文解析买家数字身份。
func parseRequestUserID(req servertypes.IRequest) (uint, error) {
	uid, _ := req.GetUser()
	userID64, err := strconv.ParseUint(uid, 10, 64)
	if err != nil || userID64 == 0 {
		return 0, errors.New("用户身份无效")
	}
	return uint(userID64), nil
}

var _ servertypes.IWebSocketUserIdentity = (*GetOrders)(nil)
var _ servertypes.IWebSocketRouterNotice = (*GetOrders)(nil)
