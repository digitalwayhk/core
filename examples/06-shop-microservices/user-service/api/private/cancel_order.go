package private

import (
	"errors"

	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	orderapi "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/public"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

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
