package private

import (
	"net/http"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/04-shop-performance/api/dto"
	"github.com/digitalwayhk/core/examples/04-shop-performance/business"
	"github.com/digitalwayhk/core/examples/04-shop-performance/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

// GetOrders 查询本人订单并作为最终用户 WebSocket 订阅路由。
type GetOrders struct {
	requestUserID      string
	subscriptionUserID string
}

// Parse 只保存认证中间件注入的可信请求身份，不读取客户端身份字段。
func (own *GetOrders) Parse(req servertypes.IRequest) error {
	userID, _ := req.GetUser()
	own.requestUserID = strings.TrimSpace(userID)
	return nil
}

// Validation 校验 HTTP 或 WebSocket 会话身份。
func (own *GetOrders) Validation(req servertypes.IRequest) error {
	if own.resolveUserID(req) == "" {
		return models.NewBusinessError("用户身份无效")
	}
	return nil
}

// Do 通过订单业务服务查询本人订单。
func (own *GetOrders) Do(req servertypes.IRequest) (interface{}, error) {
	items, err := business.NewOrderService().ListUserOrders(own.resolveUserID(req))
	if err != nil {
		return nil, err
	}
	return dto.OrderResponses(items), nil
}

// GetResponse 返回 OpenAPI 订单列表结构。
func (own *GetOrders) GetResponse() interface{} { return []*dto.OrderResponse{} }

// GetCacheKey 使用可信用户身份的哈希隔离订单缓存。
func (own *GetOrders) GetCacheKey() string {
	userID := strings.TrimSpace(own.requestUserID)
	if userID == "" {
		userID = strings.TrimSpace(own.subscriptionUserID)
	}
	if userID == "" {
		return ""
	}
	return utils.HashCodes(userID)
}

// RouterInfo 注册认证 GET 路由。
func (own *GetOrders) RouterInfo() *servertypes.RouterInfo {
	info := router.DefaultRouterInfoWithOptions(own, router.WithMethod(http.MethodGet))
	info.UseCache(10 * time.Second)
	return info
}

// Reset 在对象池复用前清理请求级身份。
func (own *GetOrders) Reset() { own.requestUserID = "" }

// Clean 在请求归池或订阅释放时清理身份。
func (own *GetOrders) Clean() {
	own.requestUserID = ""
	own.subscriptionUserID = ""
}

// SetUserID 接收 WebSocket 会话注入的可信身份。
func (own *GetOrders) SetUserID(userID, _ string) { own.subscriptionUserID = strings.TrimSpace(userID) }

// GetUserID 返回订阅绑定的用户 ID。
func (own *GetOrders) GetUserID() string { return own.subscriptionUserID }

// GetHashKey 使用用户 ID 隔离订阅组。
func (own *GetOrders) GetHashKey() uint64 { return utils.HashCode64(own.subscriptionUserID) }

// NoticeFiltersRouter 只向订单所属用户投递事件。
func (own *GetOrders) NoticeFiltersRouter(message interface{}, api servertypes.IRouter) (bool, interface{}) {
	response, ok := message.(*dto.OrderResponse)
	if !ok || response == nil {
		return false, nil
	}
	subscription, ok := api.(*GetOrders)
	if !ok || subscription.subscriptionUserID == "" || response.UserID != subscription.subscriptionUserID {
		return false, nil
	}
	return true, response
}

// resolveUserID 优先使用 HTTP 身份，并在订阅执行时回退到会话身份。
func (own *GetOrders) resolveUserID(req servertypes.IRequest) string {
	if userID := strings.TrimSpace(own.requestUserID); userID != "" {
		return userID
	}
	if req != nil {
		if userID, _ := req.GetUser(); strings.TrimSpace(userID) != "" {
			return strings.TrimSpace(userID)
		}
	}
	return strings.TrimSpace(own.subscriptionUserID)
}
