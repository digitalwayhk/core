package public

import (
	"errors"
	"net/http"
	"strconv"

	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/business"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

type CancelOrder struct {
	UserID  uint `json:"userID"`
	OrderID uint `json:"orderID"`
}

func (own *CancelOrder) Parse(req servertypes.IRequest) error { return req.Bind(own) }

func (own *CancelOrder) Validation(servertypes.IRequest) error {
	if own.UserID == 0 || own.OrderID == 0 {
		return errors.New("用户和订单不能为空")
	}
	return nil
}

func (own *CancelOrder) Do(req servertypes.IRequest) (interface{}, error) {
	return business.CancelOrder(own.UserID, own.OrderID, strconv.FormatUint(uint64(req.NewID()), 10))
}

func (*CancelOrder) GetResponse() interface{} { return &orderdto.Order{} }

func (own *CancelOrder) RouterInfo() *servertypes.RouterInfo {
	return orderPublicRoute(own, "cancelorder", http.MethodPost)
}
