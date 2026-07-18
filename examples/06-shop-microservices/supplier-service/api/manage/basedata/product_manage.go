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

// ProductManage 通过同一组 Manage 路由服务供应商本人和平台管理员。
type ProductManage struct {
	*BaseDataManage[models.Product]
	SetEnabled *SetProductEnabled
}

func NewProductManage() *ProductManage {
	own := &ProductManage{}
	own.BaseDataManage = NewBaseDataManage[models.Product](own)
	own.SetEnabled = NewSetProductEnabled(own)
	return own
}

func (own *ProductManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Add, own.Edit, own.Remove, own.SetEnabled}
}

func (*ProductManage) SupplierOwnerColumn() string { return "SupplierID" }

func (*ProductManage) ResolveSupplierWriteScope(sender interface{}, actor commonmanage.Actor) (commonmanage.WriteScope, error, bool) {
	switch operation := sender.(type) {
	case *managepkg.Add[models.Product]:
		if operation.Model == nil {
			return commonmanage.WriteScope{}, contract.ErrResourceNotFound, true
		}
		ownerID := operation.Model.SupplierID
		if !actor.Admin {
			ownerID = actor.Supplier.ID
		}
		operation.Model.SupplierID = ownerID
		supplier, findErr := models.FindSupplierByID(ownerID)
		if findErr != nil || supplier == nil {
			return commonmanage.WriteScope{}, contract.ErrResourceNotFound, true
		}
		if !supplier.Enabled {
			return commonmanage.WriteScope{}, contract.ErrSubjectDisabled, true
		}
		return commonmanage.WriteScope{Supplier: supplier}, nil, false
	case *managepkg.Edit[models.Product]:
		if operation.Model == nil || operation.OldItem == nil {
			return commonmanage.WriteScope{}, contract.ErrResourceNotFound, true
		}
		supplier, findErr := models.FindSupplierByID(operation.OldItem.SupplierID)
		if findErr != nil || supplier == nil {
			return commonmanage.WriteScope{}, contract.ErrResourceNotFound, true
		}
		return commonmanage.WriteScope{Supplier: supplier}, nil, false
	case *managepkg.Remove[models.Product]:
		if operation.Model == nil {
			return commonmanage.WriteScope{}, contract.ErrResourceNotFound, true
		}
		current, findErr := models.FindProduct(operation.Model.ID)
		if findErr != nil || current == nil {
			return commonmanage.WriteScope{}, contract.ErrResourceNotFound, true
		}
		supplier, findErr := models.FindSupplierByID(current.SupplierID)
		if findErr != nil || supplier == nil {
			return commonmanage.WriteScope{}, contract.ErrResourceNotFound, true
		}
		return commonmanage.WriteScope{Supplier: supplier}, nil, false
	case *SetProductEnabled:
		if operation.Model == nil {
			return commonmanage.WriteScope{}, contract.ErrResourceNotFound, true
		}
		current, findErr := models.FindProduct(operation.Model.ID)
		if findErr != nil || current == nil {
			return commonmanage.WriteScope{}, contract.ErrResourceNotFound, true
		}
		supplier, findErr := models.FindSupplierByID(current.SupplierID)
		if findErr != nil || supplier == nil {
			return commonmanage.WriteScope{}, contract.ErrResourceNotFound, true
		}
		return commonmanage.WriteScope{Supplier: supplier}, nil, false
	}
	return commonmanage.WriteScope{}, nil, false
}

func (own *ProductManage) OnAddBefore(operation *managepkg.Add[models.Product], req servertypes.IRequest) (interface{}, error, bool) {
	eventID := models.EventID(req.NewID())
	created, err := business.CreateProduct(operation.Model.SupplierID, operation.Model.Name, operation.Model.Code, operation.Model.Price, req.NewID(), eventID)
	return created, err, true
}

func (own *ProductManage) OnEditBefore(operation *managepkg.Edit[models.Product], req servertypes.IRequest) (interface{}, error, bool) {
	eventID := models.EventID(req.NewID())
	current := operation.OldItem
	updated, err := business.UpdateProduct(current.ID, operation.Model.Name, operation.Model.Code, operation.Model.Price, eventID)
	return updated, err, true
}

func (own *ProductManage) OnRemoveBefore(operation *managepkg.Remove[models.Product], _ servertypes.IRequest) (interface{}, error, bool) {
	current, findErr := models.FindProduct(operation.Model.ID)
	if findErr != nil || current == nil {
		return nil, contract.ErrResourceNotFound, true
	}
	err := models.DeleteProduct(current)
	return current, err, true
}

func (own *ProductManage) OnCommandBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	switch operation := sender.(type) {
	case *SetProductEnabled:
		current, findErr := models.FindProduct(operation.Model.ID)
		if findErr != nil || current == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		eventID := models.EventID(req.NewID())
		updated, err := business.SetProductEnabled(current.ID, operation.Model.Enabled, eventID)
		return updated, err, true
	}
	return nil, nil, false
}

func (*ProductManage) OnDoAfter(interface{}, servertypes.IRequest) (interface{}, error) {
	return nil, nil
}

func (*ProductManage) ViewModel(model *view.ViewModel) {
	model.Title = "商品管理"
	model.AutoLoad = true
}

func (*ProductManage) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	if field.IsFieldOrTitle("SupplierID") || field.IsFieldOrTitle("Enabled") {
		field.IsEdit = false
	}
}
