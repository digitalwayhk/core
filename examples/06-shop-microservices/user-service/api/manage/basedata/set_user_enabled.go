package basedata

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
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
	owner, ok := own.GetInstance().(*UserManage)
	if !ok {
		return nil, contract.ErrForbidden
	}
	result, err, stop := owner.DoBefore(own, req)
	if stop || err != nil || result != nil {
		return result, err
	}
	if own.Model == nil {
		return nil, contract.ErrResourceNotFound
	}
	current, err := models.FindUserByID(own.Model.ID)
	if err != nil || current == nil {
		return nil, contract.ErrResourceNotFound
	}
	current.TraceID = req.GetTraceId()
	current.Enabled = own.Model.Enabled
	return current, models.SaveUser(current)
}

func (own *SetUserEnabled) RouterInfo() *servertypes.RouterInfo { return managepkg.RouterInfo(own) }
