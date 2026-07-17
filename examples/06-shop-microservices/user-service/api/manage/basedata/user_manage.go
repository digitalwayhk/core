package basedata

import (
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	commonmanage "github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/api/manage/common"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

type UserManage struct {
	*BaseDataManage[models.User]
	SetEnabled *SetUserEnabled
}

func NewUserManage() *UserManage {
	own := &UserManage{}
	own.BaseDataManage = NewBaseDataManage[models.User](own)
	own.SetEnabled = NewSetUserEnabled(own)
	return own
}

func (own *UserManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Edit, own.SetEnabled}
}

func (*UserManage) SearchBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	search, ok := sender.(*managepkg.Search[models.User])
	if !ok {
		return nil, contract.ErrResourceNotFound, true
	}
	return commonmanage.OwnerSearch(search.SearchItem, req, "ID")
}

func (own *UserManage) DoBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	actor, err := commonmanage.ActorFrom(req)
	if err != nil {
		return nil, err, true
	}
	switch operation := sender.(type) {
	case *managepkg.Edit[models.User]:
		if operation.Model == nil || operation.OldItem == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		if err := commonmanage.AuthorizeWrite(actor, operation.OldItem.ID); err != nil {
			return nil, err, true
		}
		current := operation.OldItem
		current.Name = strings.TrimSpace(operation.Model.Name)
		return current, models.SaveUser(current), true
	case *SetUserEnabled:
		if !actor.Admin {
			return nil, contract.ErrForbidden, true
		}
		if operation.Model == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		current, err := models.FindUserByID(operation.Model.ID)
		if err != nil || current == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		current.Enabled = operation.Model.Enabled
		return current, models.SaveUser(current), true
	}
	return nil, nil, false
}

func (*UserManage) ViewModel(model *view.ViewModel) {
	model.Title, model.AutoLoad = "用户管理", true
}

func (*UserManage) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	if field.IsFieldOrTitle("AuthUserID") || field.IsFieldOrTitle("Enabled") {
		field.IsEdit = false
	}
}
