// Package private 提供 07 用户入口服务买家撤单 API。
package private

import (
	"errors"
	"net/http"
	"strconv"

	orderdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/order"
	orderapi "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/public"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// CancelOrder 是买家撤销本人订单入口。
type CancelOrder struct {
	OrderID uint `json:"orderID"`
}

// Parse 绑定撤单请求。
func (own *CancelOrder) Parse(req servertypes.IRequest) error { return req.Bind(own) }

// Validation 校验撤单请求。
func (own *CancelOrder) Validation(servertypes.IRequest) error {
	if own.OrderID == 0 {
		return errors.New("订单 ID 不能为空")
	}
	return nil
}

// Do 调用订单服务内部撤单 API。
func (own *CancelOrder) Do(req servertypes.IRequest) (interface{}, error) {
	uid, _ := req.GetUser()
	userID64, err := strconv.ParseUint(uid, 10, 64)
	if err != nil || userID64 == 0 {
		return nil, errors.New("用户身份无效")
	}
	response, err := req.CallService(&orderapi.CancelOrder{UserID: uint(userID64), OrderID: own.OrderID})
	if err != nil || !response.GetSuccess() {
		if err != nil {
			return nil, err
		}
		return nil, response.GetError()
	}
	var order orderdto.Order
	response.GetData(&order)
	return &order, nil
}

// GetResponse 返回撤单响应 DTO 类型。
func (*CancelOrder) GetResponse() interface{} { return &orderdto.Order{} }

// RouterInfo 返回买家撤单 Private 路由信息。
func (own *CancelOrder) RouterInfo() *servertypes.RouterInfo {
	return userPrivateRoute(own, "cancelorder", http.MethodPost)
}
