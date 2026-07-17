package manage

import (
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	publicapi "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/api/public"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/business"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

type manageActor struct {
	admin    bool
	supplier *models.Supplier
}

func actorFromRequest(req servertypes.IRequest) (manageActor, error) {
	uid, _ := req.GetUser()
	uid = strings.TrimSpace(uid)
	if uid == contract.PlatformAdminUserID {
		return manageActor{admin: true}, nil
	}
	if uid == "" {
		return manageActor{}, contract.ErrInvalidIdentity
	}
	supplier, err := models.FindSupplier(uid)
	if err != nil || supplier == nil {
		return manageActor{}, contract.ErrResourceNotFound
	}
	return manageActor{supplier: supplier}, nil
}

func addOwnerWhere(item *view.SearchItem, req servertypes.IRequest, column string) (interface{}, error, bool) {
	actor, err := actorFromRequest(req)
	if err != nil {
		return nil, err, true
	}
	if actor.admin {
		return nil, nil, false
	}
	if item == nil {
		return nil, contract.ErrResourceNotFound, true
	}
	item.WhereList = append(item.WhereList, &view.SearchWhere{Name: column, Value: actor.supplier.ID})
	return nil, nil, false
}

func authorizeSupplierWrite(actor manageActor, supplier *models.Supplier) error {
	if supplier == nil {
		return contract.ErrResourceNotFound
	}
	if actor.admin {
		return nil
	}
	if actor.supplier == nil || actor.supplier.ID != supplier.ID {
		return contract.ErrForbidden
	}
	if !actor.supplier.Enabled {
		return contract.ErrSubjectDisabled
	}
	return nil
}

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
	return addOwnerWhere(search.SearchItem, req, "ID")
}

func (own *SupplierManage) DoBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	actor, err := actorFromRequest(req)
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
		if err := authorizeSupplierWrite(actor, current); err != nil {
			return nil, err, true
		}
		updated, err := business.UpdateSupplierDetails(current.ID, operation.Model.Name, operation.Model.Code, operation.Model.Description, eventID)
		if err == nil {
			publicapi.InvalidateSupplierCaches()
		}
		return updated, err, true
	case *managepkg.Remove[models.Supplier]:
		if !actor.admin {
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
		if !actor.admin {
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

// OrderManage 只提供供应商订单投影的查询能力。
type OrderManage struct {
	*managepkg.ManageService[models.SupplierOrder]
}

func NewOrderManage() *OrderManage {
	own := &OrderManage{}
	own.ManageService = managepkg.NewManageService[models.SupplierOrder](own)
	return own
}

func (own *OrderManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search}
}

func (*OrderManage) SearchBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	search, ok := sender.(*managepkg.Search[models.SupplierOrder])
	if !ok {
		return nil, contract.ErrResourceNotFound, true
	}
	return addOwnerWhere(search.SearchItem, req, "SupplierID")
}

func (*OrderManage) ViewModel(model *view.ViewModel) {
	model.Title = "供应商订单查询"
	model.AutoLoad = true
}
