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

type GetOrders struct {
	requestUserID      uint
	subscriptionUserID uint
	subscriptionAuthID string
}

func (*GetOrders) Parse(servertypes.IRequest) error { return nil }

func (own *GetOrders) Validation(req servertypes.IRequest) error {
	user, err := trustedUser(req, false)
	if err != nil {
		return err
	}
	own.requestUserID = user.ID
	return nil
}

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

func (*GetOrders) GetResponse() interface{} { return []*orderdto.Order{} }

func (own *GetOrders) RouterInfo() *servertypes.RouterInfo {
	info := router.DefaultRouterInfoWithOptions(own, router.WithMethod(http.MethodGet))
	info.UseCache(10 * time.Second)
	return info
}

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

func InvalidateOrderCache(userID uint) {
	(&GetOrders{}).RouterInfo().FailureCache(&GetOrders{requestUserID: userID})
}

func (own *GetOrders) Reset() { own.requestUserID = 0 }
func (own *GetOrders) Clean() { own.requestUserID = 0 }

func (own *GetOrders) SetUserID(uid, _ string) {
	own.subscriptionAuthID = strings.TrimSpace(uid)
	if user, err := models.FindUser(own.subscriptionAuthID); err == nil && user != nil {
		own.subscriptionUserID = user.ID
	}
}

func (own *GetOrders) GetUserID() string  { return own.subscriptionAuthID }
func (own *GetOrders) GetHashKey() uint64 { return uint64(own.subscriptionUserID) }

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
