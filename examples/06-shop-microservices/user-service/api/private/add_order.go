package private

import (
	"errors"
	"strconv"
	"strings"

	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	orderapi "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/public"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

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
