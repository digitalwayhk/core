package manage

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
)

type SetUserEnabled struct {
	managepkg.Operation[models.User]
}

func NewSetUserEnabled(owner interface{}) *SetUserEnabled {
	return &SetUserEnabled{Operation: managepkg.NewOperation[models.User](owner)}
}

func (own *SetUserEnabled) New(instance interface{}) servertypes.IRouter {
	next := NewSetUserEnabled(nil)
	next.Operation.New(instance)
	return next
}

func (own *SetUserEnabled) Do(req servertypes.IRequest) (interface{}, error) {
	result, err, _ := own.GetInstance().(*UserManage).DoBefore(own, req)
	return result, err
}

func (own *SetUserEnabled) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }
