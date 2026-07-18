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

func NewSupplierManage() *SupplierManage {
	own := &SupplierManage{}
	own.BaseDataManage = NewBaseDataManage[models.Supplier](own)
	own.SetEnabled = NewSetSupplierEnabled(own)
	return own
}

func (own *SupplierManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Edit, own.Remove, own.SetEnabled}
}

func (*SupplierManage) SupplierOwnerColumn() string { return "ID" }

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

func (own *SupplierManage) OnEditBefore(operation *managepkg.Edit[models.Supplier], req servertypes.IRequest) (interface{}, error, bool) {
	eventID := models.EventID(req.NewID())
	current := operation.OldItem
	updated, err := business.UpdateSupplierDetails(current.ID, operation.Model.Name, operation.Model.Code, operation.Model.Description, eventID)
	return updated, err, true
}

func (own *SupplierManage) OnRemoveBefore(operation *managepkg.Remove[models.Supplier], _ servertypes.IRequest) (interface{}, error, bool) {
	current, findErr := models.FindSupplierByID(operation.Model.ID)
	if findErr != nil || current == nil {
		return nil, contract.ErrResourceNotFound, true
	}
	err := models.DeleteSupplier(current)
	return current, err, true
}

func (own *SupplierManage) OnCommandBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	switch operation := sender.(type) {
	case *SetSupplierEnabled:
		eventID := models.EventID(req.NewID())
		updated, err := business.SetSupplierEnabled(operation.Model.ID, operation.Model.Enabled, eventID)
		return updated, err, true
	}
	return nil, nil, false
}

func (*SupplierManage) OnDoAfter(interface{}, servertypes.IRequest) (interface{}, error) {
	return nil, nil
}

func (*SupplierManage) ViewModel(model *view.ViewModel) {
	model.Title = "供应商管理"
	model.AutoLoad = true
}

func (*SupplierManage) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	if field.IsFieldOrTitle("AuthUserID") || field.IsFieldOrTitle("Enabled") {
		field.IsEdit = false
	}
}
