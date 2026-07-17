package public

import (
	"errors"
	"net/http"

	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/business"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

type GetOrders struct {
	UserID uint `json:"userID"`
}

func (own *GetOrders) Parse(req servertypes.IRequest) error { return req.Bind(own) }

func (own *GetOrders) Validation(servertypes.IRequest) error {
	if own.UserID == 0 {
		return errors.New("用户不能为空")
	}
	return nil
}

func (own *GetOrders) Do(servertypes.IRequest) (interface{}, error) {
	return business.UserOrders(own.UserID)
}

func (*GetOrders) GetResponse() interface{} { return []*orderdto.Order{} }

func (own *GetOrders) RouterInfo() *servertypes.RouterInfo {
	return orderPublicRoute(own, "getorders", http.MethodPost)
}
