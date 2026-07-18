// 本文件提供用户服务面向普通用户的 Private API 编排能力。
package private

import (
	"errors"

	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	orderapi "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/public"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// CancelOrder 定义本文件能力使用的核心结构。
type CancelOrder struct {
	OrderID uint `json:"orderID"`
}

// Parse 实现本类型在当前服务边界中的行为。
func (own *CancelOrder) Parse(req servertypes.IRequest) error { return req.Bind(own) }

// Validation 实现本类型在当前服务边界中的行为。
func (own *CancelOrder) Validation(req servertypes.IRequest) error {
	if own.OrderID == 0 {
		return errors.New("订单 ID 不能为空")
	}
	_, err := trustedUser(req, true)
	return err
}

// Do 实现本类型在当前服务边界中的行为。
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

// GetResponse 实现本类型在当前服务边界中的行为。
func (*CancelOrder) GetResponse() interface{} { return &orderdto.Order{} }

// RouterInfo 实现本类型在当前服务边界中的行为。
func (own *CancelOrder) RouterInfo() *servertypes.RouterInfo { return router.DefaultRouterInfo(own) }
