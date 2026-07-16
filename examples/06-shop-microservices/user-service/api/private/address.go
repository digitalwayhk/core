package private

import (
	"errors"
	"net/http"
	"strings"

	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

func trustedUser(req servertypes.IRequest) (string, error) {
	uid, _ := req.GetUser()
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return "", errors.New("用户身份无效")
	}
	return uid, nil
}

type AddAddress struct {
	Recipient string `json:"recipient"`
	Phone     string `json:"phone"`
	Region    string `json:"region"`
	Detail    string `json:"detail"`
}

func (a *AddAddress) Parse(req servertypes.IRequest) error { return req.Bind(a) }
func (a *AddAddress) Validation(req servertypes.IRequest) error {
	_, err := trustedUser(req)
	return err
}
func (a *AddAddress) Do(req servertypes.IRequest) (interface{}, error) {
	uid, _ := trustedUser(req)
	item := models.NewAddress()
	item.SetID(req.NewID())
	item.UserID = uid
	item.Recipient = a.Recipient
	item.Phone = a.Phone
	item.Region = a.Region
	item.Detail = a.Detail
	if err := models.InsertAddress(item); err != nil {
		return nil, err
	}
	return models.AddressDTO(item), nil
}
func (*AddAddress) GetResponse() interface{}              { return &userdto.Address{} }
func (a *AddAddress) RouterInfo() *servertypes.RouterInfo { return router.DefaultRouterInfo(a) }

type GetAddresses struct{}

func (*GetAddresses) Parse(servertypes.IRequest) error { return nil }
func (*GetAddresses) Validation(req servertypes.IRequest) error {
	_, err := trustedUser(req)
	return err
}
func (*GetAddresses) Do(req servertypes.IRequest) (interface{}, error) {
	uid, _ := trustedUser(req)
	items, err := models.ListAddresses(uid)
	if err != nil {
		return nil, err
	}
	result := make([]*userdto.Address, 0, len(items))
	for _, item := range items {
		result = append(result, models.AddressDTO(item))
	}
	return result, nil
}
func (*GetAddresses) GetResponse() interface{} { return []*userdto.Address{} }
func (g *GetAddresses) RouterInfo() *servertypes.RouterInfo {
	return router.DefaultRouterInfoWithOptions(g, router.WithMethod(http.MethodGet))
}

type DeleteAddress struct {
	AddressID uint `json:"addressID"`
}

func (d *DeleteAddress) Parse(req servertypes.IRequest) error { return req.Bind(d) }
func (d *DeleteAddress) Validation(req servertypes.IRequest) error {
	if d.AddressID == 0 {
		return errors.New("地址 ID 不能为空")
	}
	_, err := trustedUser(req)
	return err
}
func (d *DeleteAddress) Do(req servertypes.IRequest) (interface{}, error) {
	uid, _ := trustedUser(req)
	item, err := models.FindOwnedAddress(uid, d.AddressID)
	if err != nil || item == nil {
		return nil, errors.New("地址不存在或无权操作")
	}
	return true, models.DeleteAddress(item)
}
func (*DeleteAddress) GetResponse() interface{}              { return true }
func (d *DeleteAddress) RouterInfo() *servertypes.RouterInfo { return router.DefaultRouterInfo(d) }
