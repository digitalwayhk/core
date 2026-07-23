// 本文件提供当前服务基础资料 Manage API 的对象管理和受控命令能力。
package basedata

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	commonmanage "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/api/manage/common"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/business"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// SupplierManage 同时服务供应商本人和平台管理员，权限差异全部由 Hook 处理。
type SupplierManage struct {
	*BaseDataManage[models.Supplier]
	SetEnabled *SetSupplierEnabled
}

// NewSupplierManage 执行本文件能力对应的业务操作。
func NewSupplierManage() *SupplierManage {
	own := &SupplierManage{}
	own.BaseDataManage = NewBaseDataManage[models.Supplier](own)
	own.SetEnabled = NewSetSupplierEnabled(own)
	return own
}

// Routers 实现本类型在当前服务边界中的行为。
func (own *SupplierManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Edit, own.Remove, own.SetEnabled}
}

// SupplierOwnerColumn 实现本类型在当前服务边界中的行为。
func (*SupplierManage) SupplierOwnerColumn() string { return "ID" }

// ResolveSupplierWriteScope 实现本类型在当前服务边界中的行为。
func (*SupplierManage) ResolveSupplierWriteScope(sender interface{}, _ commonmanage.Actor) (commonmanage.WriteScope, error, bool) {
	switch operation := sender.(type) {
	case *managepkg.Edit[models.Supplier]:
		if operation.Model == nil || operation.OldItem == nil {
			return commonmanage.WriteScope{}, contract.ErrResourceNotFound, true
		}
		return commonmanage.WriteScope{Supplier: operation.OldItem}, nil, false
	case *managepkg.Remove[models.Supplier]:
		if operation.Model == nil {
			return commonmanage.WriteScope{}, contract.ErrResourceNotFound, true
		}
		return commonmanage.WriteScope{AdminOnly: true}, nil, false
	case *SetSupplierEnabled:
		if operation.Model == nil {
			return commonmanage.WriteScope{}, contract.ErrResourceNotFound, true
		}
		return commonmanage.WriteScope{AdminOnly: true}, nil, false
	}
	return commonmanage.WriteScope{}, nil, false
}

// OnEditBefore 实现本类型在当前服务边界中的行为。
func (own *SupplierManage) OnEditBefore(operation *managepkg.Edit[models.Supplier], req servertypes.IRequest) (interface{}, error, bool) {
	eventID := models.EventID(req.NewID())
	current := operation.OldItem
	updated, err := business.UpdateSupplierDetails(current.ID, operation.Model.Name, operation.Model.Code, operation.Model.Description, req.GetTraceId(), eventID)
	return updated, err, true
}

// OnRemoveBefore 实现本类型在当前服务边界中的行为。
func (own *SupplierManage) OnRemoveBefore(operation *managepkg.Remove[models.Supplier], _ servertypes.IRequest) (interface{}, error, bool) {
	current, findErr := models.FindSupplierByID(operation.Model.ID)
	if findErr != nil || current == nil {
		return nil, contract.ErrResourceNotFound, true
	}
	err := models.DeleteSupplier(current)
	return current, err, true
}

// OnDoAfter 实现本类型在当前服务边界中的行为。
func (*SupplierManage) OnDoAfter(interface{}, servertypes.IRequest) (interface{}, error) {
	return nil, nil
}

// ViewModel 实现本类型在当前服务边界中的行为。
func (*SupplierManage) ViewModel(model *view.ViewModel) {
	model.Title = "供应商管理"
	model.AutoLoad = true
}

// ViewFieldModel 实现本类型在当前服务边界中的行为。
func (*SupplierManage) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	if field.IsFieldOrTitle("AuthUserID") || field.IsFieldOrTitle("Enabled") {
		field.IsEdit = false
	}
}
