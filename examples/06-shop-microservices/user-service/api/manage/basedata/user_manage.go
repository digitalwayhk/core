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

func (*UserManage) UserOwnerColumn() string { return "ID" }

func (*UserManage) ResolveUserWriteScope(sender interface{}, _ commonmanage.Actor) (commonmanage.WriteScope, error, bool) {
	switch operation := sender.(type) {
	case *managepkg.Edit[models.User]:
		if operation.Model == nil || operation.OldItem == nil {
			return commonmanage.WriteScope{}, contract.ErrResourceNotFound, true
		}
		return commonmanage.WriteScope{UserID: operation.OldItem.ID}, nil, false
	case *SetUserEnabled:
		if operation.Model == nil {
			return commonmanage.WriteScope{}, contract.ErrResourceNotFound, true
		}
		return commonmanage.WriteScope{AdminOnly: true}, nil, false
	}
	return commonmanage.WriteScope{}, nil, false
}

func (own *UserManage) OnEditBefore(operation *managepkg.Edit[models.User], req servertypes.IRequest) (interface{}, error, bool) {
	current := operation.OldItem
	current.TraceID = req.GetTraceId()
	current.Name = strings.TrimSpace(operation.Model.Name)
	return current, models.SaveUser(current), true
}

func (*UserManage) ViewModel(model *view.ViewModel) {
	model.Title, model.AutoLoad = "用户管理", true
}

func (*UserManage) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	if field.IsFieldOrTitle("AuthUserID") || field.IsFieldOrTitle("Enabled") {
		field.IsEdit = false
	}
}
