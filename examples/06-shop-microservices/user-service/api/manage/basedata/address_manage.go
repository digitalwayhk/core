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

func (*AddressManage) SearchBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	search, ok := sender.(*managepkg.Search[models.Address])
	if !ok {
		return nil, contract.ErrResourceNotFound, true
	}
	return commonmanage.OwnerSearch(search.SearchItem, req, "UserID")
}

func (*AddressManage) DoBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	actor, err := commonmanage.ActorFrom(req)
	if err != nil {
		return nil, err, true
	}
	switch operation := sender.(type) {
	case *managepkg.Add[models.Address]:
		if operation.Model == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		if !actor.Admin {
			operation.Model.UserID = actor.User.ID
		}
		if err := commonmanage.AuthorizeWrite(actor, operation.Model.UserID); err != nil {
			return nil, err, true
		}
	case *managepkg.Edit[models.Address]:
		if operation.Model == nil || operation.OldItem == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		if err := commonmanage.AuthorizeWrite(actor, operation.OldItem.UserID); err != nil {
			return nil, err, true
		}
		operation.Model.UserID = operation.OldItem.UserID
	case *managepkg.Remove[models.Address]:
		if operation.Model == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		current, err := models.FindAddress(operation.Model.ID)
		if err != nil || current == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		if err := commonmanage.AuthorizeWrite(actor, current.UserID); err != nil {
			return nil, err, true
		}
	}
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
