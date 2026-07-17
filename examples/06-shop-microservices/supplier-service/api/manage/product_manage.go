package manage

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	publicapi "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/api/public"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/business"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// ProductManage 通过同一组 Manage 路由服务供应商本人和平台管理员。
type ProductManage struct {
	*managepkg.ManageService[models.Product]
	SetEnabled *SetProductEnabled
}

func NewProductManage() *ProductManage {
	own := &ProductManage{}
	own.ManageService = managepkg.NewManageService[models.Product](own)
	own.SetEnabled = NewSetProductEnabled(own)
	return own
}

func (own *ProductManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Add, own.Edit, own.Remove, own.SetEnabled}
}

func (*ProductManage) SearchBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	search, ok := sender.(*managepkg.Search[models.Product])
	if !ok {
		return nil, contract.ErrResourceNotFound, true
	}
	return addOwnerWhere(search.SearchItem, req, "SupplierID")
}

func (own *ProductManage) DoBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	actor, err := actorFromRequest(req)
	if err != nil {
		return nil, err, true
	}
	eventID := models.EventID(req.NewID())
	switch operation := sender.(type) {
	case *managepkg.Add[models.Product]:
		if operation.Model == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		ownerID := operation.Model.SupplierID
		if !actor.admin {
			if !actor.supplier.Enabled {
				return nil, contract.ErrSubjectDisabled, true
			}
			ownerID = actor.supplier.ID
		}
		supplier, findErr := models.FindSupplierByID(ownerID)
		if findErr != nil || supplier == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		if !supplier.Enabled {
			return nil, contract.ErrSubjectDisabled, true
		}
		created, err := business.CreateProduct(ownerID, operation.Model.Name, operation.Model.Code, operation.Model.Price, req.NewID(), eventID)
		if err == nil {
			publicapi.InvalidateProductCache()
		}
		return created, err, true
	case *managepkg.Edit[models.Product]:
		if operation.Model == nil || operation.OldItem == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		current := operation.OldItem
		supplier, findErr := models.FindSupplierByID(current.SupplierID)
		if findErr != nil || supplier == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		if err := authorizeSupplierWrite(actor, supplier); err != nil {
			return nil, err, true
		}
		updated, err := business.UpdateProduct(current.ID, operation.Model.Name, operation.Model.Code, operation.Model.Price, eventID)
		if err == nil {
			publicapi.InvalidateProductCache()
		}
		return updated, err, true
	case *managepkg.Remove[models.Product]:
		if operation.Model == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		current, findErr := models.FindProduct(operation.Model.ID)
		if findErr != nil || current == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		supplier, findErr := models.FindSupplierByID(current.SupplierID)
		if findErr != nil || supplier == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		if err := authorizeSupplierWrite(actor, supplier); err != nil {
			return nil, err, true
		}
		err := models.DeleteProduct(current)
		if err == nil {
			publicapi.InvalidateProductCache()
		}
		return current, err, true
	case *SetProductEnabled:
		if operation.Model == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		current, findErr := models.FindProduct(operation.Model.ID)
		if findErr != nil || current == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		supplier, findErr := models.FindSupplierByID(current.SupplierID)
		if findErr != nil || supplier == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		if err := authorizeSupplierWrite(actor, supplier); err != nil {
			return nil, err, true
		}
		updated, err := business.SetProductEnabled(current.ID, operation.Model.Enabled, eventID)
		if err == nil {
			publicapi.InvalidateProductCache()
		}
		return updated, err, true
	}
	return nil, nil, false
}

func (*ProductManage) DoAfter(interface{}, servertypes.IRequest) (interface{}, error) {
	publicapi.InvalidateProductCache()
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
