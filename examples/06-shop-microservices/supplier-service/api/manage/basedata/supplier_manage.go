package basedata

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	commonmanage "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/api/manage/common"
	publicapi "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/api/public"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/business"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// SupplierManage 同时服务供应商本人和平台管理员，权限差异全部由 Hook 处理。
type SupplierManage struct {
	*managepkg.ManageService[models.Supplier]
	SetEnabled *SetSupplierEnabled
}

func NewSupplierManage() *SupplierManage {
	own := &SupplierManage{}
	own.ManageService = managepkg.NewManageService[models.Supplier](own)
	own.SetEnabled = NewSetSupplierEnabled(own)
	return own
}

func (own *SupplierManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Edit, own.Remove, own.SetEnabled}
}

func (*SupplierManage) SearchBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	search, ok := sender.(*managepkg.Search[models.Supplier])
	if !ok {
		return nil, contract.ErrResourceNotFound, true
	}
	return commonmanage.AddOwnerWhere(search.SearchItem, req, "ID")
}

func (own *SupplierManage) DoBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	actor, err := commonmanage.ActorFromRequest(req)
	if err != nil {
		return nil, err, true
	}
	eventID := models.EventID(req.NewID())
	switch operation := sender.(type) {
	case *managepkg.Edit[models.Supplier]:
		if operation.Model == nil || operation.OldItem == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		current := operation.OldItem
		if err := commonmanage.AuthorizeSupplierWrite(actor, current); err != nil {
			return nil, err, true
		}
		updated, err := business.UpdateSupplierDetails(current.ID, operation.Model.Name, operation.Model.Code, operation.Model.Description, eventID)
		if err == nil {
			publicapi.InvalidateSupplierCaches()
		}
		return updated, err, true
	case *managepkg.Remove[models.Supplier]:
		if !actor.Admin {
			return nil, contract.ErrForbidden, true
		}
		if operation.Model == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		current, findErr := models.FindSupplierByID(operation.Model.ID)
		if findErr != nil || current == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		err := models.DeleteSupplier(current)
		if err == nil {
			publicapi.InvalidateSupplierCaches()
		}
		return current, err, true
	case *SetSupplierEnabled:
		if !actor.Admin {
			return nil, contract.ErrForbidden, true
		}
		if operation.Model == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		updated, err := business.SetSupplierEnabled(operation.Model.ID, operation.Model.Enabled, eventID)
		if err == nil {
			publicapi.InvalidateSupplierCaches()
		}
		return updated, err, true
	}
	return nil, nil, false
}

func (*SupplierManage) DoAfter(interface{}, servertypes.IRequest) (interface{}, error) {
	publicapi.InvalidateSupplierCaches()
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
