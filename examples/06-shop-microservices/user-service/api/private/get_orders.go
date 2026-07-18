// 本文件提供用户服务面向普通用户的 Private API 编排能力。
package private

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	eventdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/event"
	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	orderapi "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/public"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

// GetOrders 定义本文件能力使用的核心结构。
type GetOrders struct {
	requestUserID      uint
	subscriptionUserID uint
	subscriptionAuthID string
}

// Parse 实现本类型在当前服务边界中的行为。
func (*GetOrders) Parse(servertypes.IRequest) error { return nil }

// Validation 实现本类型在当前服务边界中的行为。
func (own *GetOrders) Validation(req servertypes.IRequest) error {
	user, err := trustedUser(req, false)
	if err != nil {
		return err
	}
	own.requestUserID = user.ID
	return nil
}

// Do 实现本类型在当前服务边界中的行为。
func (own *GetOrders) Do(req servertypes.IRequest) (interface{}, error) {
	response, err := req.CallService(&orderapi.GetOrders{UserID: own.requestUserID})
	if err != nil {
		return nil, err
	}
	if !response.GetSuccess() {
		return nil, response.GetError()
	}
	items := []*orderdto.Order{}
	response.GetData(&items)
	return items, nil
}

// GetResponse 实现本类型在当前服务边界中的行为。
func (*GetOrders) GetResponse() interface{} { return []*orderdto.Order{} }

// RouterInfo 实现本类型在当前服务边界中的行为。
func (own *GetOrders) RouterInfo() *servertypes.RouterInfo {
	info := router.DefaultRouterInfoWithOptions(own, router.WithMethod(http.MethodGet))
	info.UseCache(10 * time.Second)
	return info
}

// GetCacheKey 实现本类型在当前服务边界中的行为。
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

// InvalidateOrderCache 执行本文件能力对应的业务操作。
func InvalidateOrderCache(userID uint) {
	(&GetOrders{}).RouterInfo().FailureCache(&GetOrders{requestUserID: userID})
}

// Reset 实现本类型在当前服务边界中的行为。
func (own *GetOrders) Reset() { own.requestUserID = 0 }

// Clean 实现本类型在当前服务边界中的行为。
func (own *GetOrders) Clean() { own.requestUserID = 0 }

// SetUserID 实现本类型在当前服务边界中的行为。
func (own *GetOrders) SetUserID(uid, _ string) {
	own.subscriptionAuthID = strings.TrimSpace(uid)
	if user, err := models.FindUser(own.subscriptionAuthID); err == nil && user != nil {
		own.subscriptionUserID = user.ID
	}
}

// GetUserID 实现本类型在当前服务边界中的行为。
func (own *GetOrders) GetUserID() string { return own.subscriptionAuthID }

// GetHashKey 实现本类型在当前服务边界中的行为。
func (own *GetOrders) GetHashKey() uint64 { return uint64(own.subscriptionUserID) }

// NoticeFiltersRouter 实现本类型在当前服务边界中的行为。
func (*GetOrders) NoticeFiltersRouter(message interface{}, api servertypes.IRouter) (bool, interface{}) {
	event, ok := message.(*eventdto.OrderChanged)
	if !ok || event == nil {
		return false, nil
	}
	subscription, ok := api.(*GetOrders)
	if !ok || subscription.subscriptionUserID == 0 || event.UserID != subscription.subscriptionUserID {
		return false, nil
	}
	return true, event
}
