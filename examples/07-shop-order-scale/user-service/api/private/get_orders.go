// Package private 提供 07 用户入口服务买家订单查询 API。
package private

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	orderdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/order"
	orderapi "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/public"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// GetOrders 是买家查询本人订单入口。
type GetOrders struct {
	Page int `json:"page"`
	Size int `json:"size"`
}

// Parse 绑定订单查询请求。
func (own *GetOrders) Parse(req servertypes.IRequest) error { return req.Bind(own) }

// Validation 校验订单查询请求。
func (*GetOrders) Validation(servertypes.IRequest) error { return nil }

// Do 调用订单服务内部查询 API，只传 Token 映射出的数字 UserID。
func (own *GetOrders) Do(req servertypes.IRequest) (interface{}, error) {
	uid, _ := req.GetUser()
	userID64, err := strconv.ParseUint(uid, 10, 64)
	if err != nil || userID64 == 0 {
		return nil, errors.New("用户身份无效")
	}
	response, err := req.CallService(&orderapi.GetOrders{UserID: uint(userID64), Page: own.Page, Size: own.Size})
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
