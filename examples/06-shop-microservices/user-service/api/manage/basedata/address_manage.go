// 本文件提供当前服务基础资料 Manage API 的对象管理和受控命令能力。
package basedata

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	commonmanage "github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/api/manage/common"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// AddressManage 定义本文件能力使用的核心结构。
type AddressManage struct {
	*BaseDataManage[models.Address]
}

// NewAddressManage 执行本文件能力对应的业务操作。
func NewAddressManage() *AddressManage {
	own := &AddressManage{}
	own.BaseDataManage = NewBaseDataManage[models.Address](own)
	return own
}

// Routers 实现本类型在当前服务边界中的行为。
func (own *AddressManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Add, own.Edit, own.Remove}
}

// UserOwnerColumn 实现本类型在当前服务边界中的行为。
func (*AddressManage) UserOwnerColumn() string { return "UserID" }

// ResolveUserWriteScope 实现本类型在当前服务边界中的行为。
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

// OnAddBefore 实现本类型在当前服务边界中的行为。
func (*AddressManage) OnAddBefore(operation *managepkg.Add[models.Address], req servertypes.IRequest) (interface{}, error, bool) {
	operation.Model.TraceID = req.GetTraceId()
	return nil, nil, false
}

// OnEditBefore 实现本类型在当前服务边界中的行为。
func (*AddressManage) OnEditBefore(operation *managepkg.Edit[models.Address], req servertypes.IRequest) (interface{}, error, bool) {
	operation.Model.TraceID = req.GetTraceId()
	operation.Model.UserID = operation.OldItem.UserID
	return nil, nil, false
}

// ViewModel 实现本类型在当前服务边界中的行为。
func (*AddressManage) ViewModel(model *view.ViewModel) {
	model.Title, model.AutoLoad = "地址管理", true
}

// ViewFieldModel 实现本类型在当前服务边界中的行为。
func (*AddressManage) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	if field.IsFieldOrTitle("UserID") {
		field.IsEdit = false
	}
}
