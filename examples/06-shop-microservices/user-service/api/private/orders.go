package private

import (
	"errors"
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

func trustedUser(req servertypes.IRequest, requireEnabled bool) (*models.User, error) {
	if req == nil {
		return nil, errors.New("用户身份无效")
	}
	uid, _ := req.GetUser()
	user, err := models.FindUser(strings.TrimSpace(uid))
	if err != nil || user == nil {
		return nil, errors.New("用户身份无效")
	}
	if requireEnabled && !user.Enabled {
		return nil, errors.New("用户已禁用，只允许查看")
	}
	return user, nil
}

type AddOrder struct {
	RequestID string `json:"requestID"`
	ProductID uint   `json:"productID"`
	Quantity  int    `json:"quantity"`
	AddressID uint   `json:"addressID"`
}

func (own *AddOrder) Parse(req servertypes.IRequest) error { return req.Bind(own) }
func (own *AddOrder) Validation(req servertypes.IRequest) error {
	if strings.TrimSpace(own.RequestID) == "" {
		return errors.New("requestID 不能为空")
	}
	if own.ProductID == 0 || own.Quantity <= 0 || own.AddressID == 0 {
		return errors.New("商品、数量和地址不能为空")
	}
	_, err := trustedUser(req, true)
	return err
}
func (own *AddOrder) Do(req servertypes.IRequest) (interface{}, error) {
	user, err := trustedUser(req, true)
	if err != nil {
		return nil, err
	}
	address, err := models.FindOwnedAddress(user.ID, own.AddressID)
	if err != nil || address == nil {
		return nil, errors.New("地址不存在或无权使用")
	}
	requestID := strconv.FormatUint(uint64(user.ID), 10) + ":" + strings.TrimSpace(own.RequestID)
	response, err := req.CallService(&orderapi.CreateOrder{UserID: user.ID, ProductID: own.ProductID, Quantity: own.Quantity, RequestID: requestID, Address: models.AddressSnapshot(address)})
	if err != nil {
		return nil, err
	}
	if !response.GetSuccess() {
		return nil, response.GetError()
	}
	result := &orderdto.Order{}
	response.GetData(result)
	return result, nil
}
func (*AddOrder) GetResponse() interface{}                { return &orderdto.Order{} }
func (own *AddOrder) RouterInfo() *servertypes.RouterInfo { return router.DefaultRouterInfo(own) }

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

type CancelOrder struct {
	OrderID uint `json:"orderID"`
}

func (own *CancelOrder) Parse(req servertypes.IRequest) error { return req.Bind(own) }
func (own *CancelOrder) Validation(req servertypes.IRequest) error {
	if own.OrderID == 0 {
		return errors.New("订单 ID 不能为空")
	}
	_, err := trustedUser(req, true)
	return err
}
func (own *CancelOrder) Do(req servertypes.IRequest) (interface{}, error) {
	user, err := trustedUser(req, true)
	if err != nil {
		return nil, err
	}
	response, err := req.CallService(&orderapi.CancelOrder{UserID: user.ID, OrderID: own.OrderID})
	if err != nil {
		return nil, err
	}
	if !response.GetSuccess() {
		return nil, response.GetError()
	}
	result := &orderdto.Order{}
	response.GetData(result)
	return result, nil
}
func (*CancelOrder) GetResponse() interface{}                { return &orderdto.Order{} }
func (own *CancelOrder) RouterInfo() *servertypes.RouterInfo { return router.DefaultRouterInfo(own) }
