package private

import (
	"hash/fnv"
	"strings"

	"github.com/digitalwayhk/core/examples/01-simple-shop/api/dto"
	"github.com/digitalwayhk/core/examples/01-simple-shop/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// notifyOrderChange 通过当前 ServiceContext 中已冻结的订单路由发布用户通知。
func notifyOrderChange(req servertypes.IRequest, response *dto.OrderResponse) {
	serviceContext := router.GetContext(req.ServiceName())
	if serviceContext == nil || serviceContext.Router == nil {
		return
	}
	info := serviceContext.Router.GetRouter("/api/shop/getorders")
	if info != nil {
		info.NoticeWebSocket(response)
	}
}

// GetOrders 查询当前登录用户的订单，并作为该用户的 WebSocket 订阅路由。
type GetOrders struct {
	subscriptionUserID string
}

// Parse 不接受客户端身份参数，身份只能来自认证上下文或 WebSocket 登录会话。
func (own *GetOrders) Parse(servertypes.IRequest) error {
	return nil
}

// Validation 验证 HTTP 认证上下文或 WebSocket 订阅会话中的用户身份。
func (own *GetOrders) Validation(req servertypes.IRequest) error {
	if own.resolveUserID(req) == "" {
		return models.NewBusinessError("用户身份无效")
	}
	return nil
}

// Do 只按可信请求或订阅身份查询订单，不接受客户端身份参数。
func (own *GetOrders) Do(req servertypes.IRequest) (interface{}, error) {
	orders, err := models.NewOrder().QueryByUser(own.resolveUserID(req))
	if err != nil {
		return nil, err
	}
	return dto.OrderResponses(orders), nil
}

// GetResponse 返回 OpenAPI 用的本人订单列表成功响应结构。
func (own *GetOrders) GetResponse() interface{} {
	return []*dto.OrderResponse{}
}

// RouterInfo 将本人订单查询注册为需要认证的 GET 路由。
func (own *GetOrders) RouterInfo() *servertypes.RouterInfo {
	info := router.DefaultRouterInfo(own)
	info.Method = "GET"
	return info
}

// resolveUserID 优先读取 HTTP 认证上下文，WebSocket 订阅时回退到会话注入的身份。
func (own *GetOrders) resolveUserID(req servertypes.IRequest) string {
	if req != nil {
		if userID, _ := req.GetUser(); strings.TrimSpace(userID) != "" {
			return strings.TrimSpace(userID)
		}
	}
	return strings.TrimSpace(own.subscriptionUserID)
}

// SetUserID 接收 WebSocket logon 会话解析出的可信用户身份。
func (own *GetOrders) SetUserID(userID, _ string) {
	own.subscriptionUserID = strings.TrimSpace(userID)
}

// GetUserID 返回当前订阅绑定的可信用户 ID。
func (own *GetOrders) GetUserID() string {
	return own.subscriptionUserID
}

// GetHashKey 以 UserID 生成稳定订阅哈希，实现不同用户的订阅分组。
func (own *GetOrders) GetHashKey() uint64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(own.subscriptionUserID))
	return hash.Sum64()
}

// Reset 在对象池重用前清除 WebSocket 订阅身份。
func (own *GetOrders) Reset() {
	own.subscriptionUserID = ""
}

// Clean 在对象归还路由池前尽早清除 WebSocket 订阅身份。
func (own *GetOrders) Clean() {
	own.subscriptionUserID = ""
}

// NoticeFiltersRouter 只允许订单事件投递给同一用户的订阅实例。
func (own *GetOrders) NoticeFiltersRouter(message interface{}, api servertypes.IRouter) (bool, interface{}) {
	response, ok := message.(*dto.OrderResponse)
	if !ok || response == nil {
		return false, nil
	}
	subscription, ok := api.(*GetOrders)
	if !ok || subscription.subscriptionUserID == "" {
		return false, nil
	}
	if response.UserID != subscription.subscriptionUserID {
		return false, nil
	}
	return true, response
}
