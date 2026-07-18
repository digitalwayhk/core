// 本文件提供当前服务基础资料 Manage API 的对象管理和受控命令能力。
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

// UserManage 定义本文件能力使用的核心结构。
type UserManage struct {
	*BaseDataManage[models.User]
	SetEnabled *SetUserEnabled
}

// NewUserManage 执行本文件能力对应的业务操作。
func NewUserManage() *UserManage {
	own := &UserManage{}
	own.BaseDataManage = NewBaseDataManage[models.User](own)
	own.SetEnabled = NewSetUserEnabled(own)
	return own
}

// Routers 实现本类型在当前服务边界中的行为。
func (own *UserManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Edit, own.SetEnabled}
}

// UserOwnerColumn 实现本类型在当前服务边界中的行为。
func (*UserManage) UserOwnerColumn() string { return "ID" }

// ResolveUserWriteScope 实现本类型在当前服务边界中的行为。
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

// OnEditBefore 实现本类型在当前服务边界中的行为。
func (own *UserManage) OnEditBefore(operation *managepkg.Edit[models.User], req servertypes.IRequest) (interface{}, error, bool) {
	current := operation.OldItem
	current.TraceID = req.GetTraceId()
	current.Name = strings.TrimSpace(operation.Model.Name)
	return current, models.SaveUser(current), true
}

// ViewModel 实现本类型在当前服务边界中的行为。
func (*UserManage) ViewModel(model *view.ViewModel) {
	model.Title, model.AutoLoad = "用户管理", true
}

// ViewFieldModel 实现本类型在当前服务边界中的行为。
func (*UserManage) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	if field.IsFieldOrTitle("AuthUserID") || field.IsFieldOrTitle("Enabled") {
		field.IsEdit = false
	}
}
