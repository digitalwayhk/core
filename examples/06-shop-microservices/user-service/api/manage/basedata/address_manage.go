package basedata

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	commonmanage "github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/api/manage/common"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

type AddressManage struct {
	*BaseDataManage[models.Address]
}

func NewAddressManage() *AddressManage {
	own := &AddressManage{}
	own.BaseDataManage = NewBaseDataManage[models.Address](own)
	return own
}

func (own *AddressManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Add, own.Edit, own.Remove}
}

func (*AddressManage) UserOwnerColumn() string { return "UserID" }

func (*AddressManage) ResolveUserWriteScope(sender interface{}, actor commonmanage.Actor) (commonmanage.WriteScope, error, bool) {
	switch operation := sender.(type) {
	case *managepkg.Add[models.Address]:
		if operation.Model == nil {
			return commonmanage.WriteScope{}, contract.ErrResourceNotFound, true
		}
		if !actor.Admin {
			operation.Model.UserID = actor.User.ID
		}
		return commonmanage.WriteScope{UserID: operation.Model.UserID}, nil, false
	case *managepkg.Edit[models.Address]:
		if operation.Model == nil || operation.OldItem == nil {
			return commonmanage.WriteScope{}, contract.ErrResourceNotFound, true
		}
		return commonmanage.WriteScope{UserID: operation.OldItem.UserID}, nil, false
	case *managepkg.Remove[models.Address]:
		if operation.Model == nil {
			return commonmanage.WriteScope{}, contract.ErrResourceNotFound, true
		}
		current, err := models.FindAddress(operation.Model.ID)
		if err != nil || current == nil {
			return commonmanage.WriteScope{}, contract.ErrResourceNotFound, true
		}
		return commonmanage.WriteScope{UserID: current.UserID}, nil, false
	}
	return commonmanage.WriteScope{}, nil, false
}

func (*AddressManage) OnEditBefore(operation *managepkg.Edit[models.Address], _ servertypes.IRequest) (interface{}, error, bool) {
	operation.Model.UserID = operation.OldItem.UserID
	return nil, nil, false
}

func (*AddressManage) ViewModel(model *view.ViewModel) {
	model.Title, model.AutoLoad = "地址管理", true
}

func (*AddressManage) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	if field.IsFieldOrTitle("UserID") {
		field.IsEdit = false
	}
}
