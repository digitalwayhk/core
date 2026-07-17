package manage

import (
	"strconv"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	publicapi "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/public"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/business"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

type PaymentTypeManage struct {
	*managepkg.ManageService[models.PaymentType]
	SetEnabled *SetPaymentTypeEnabled
}

func NewPaymentTypeManage() *PaymentTypeManage {
	own := &PaymentTypeManage{}
	own.ManageService = managepkg.NewManageService[models.PaymentType](own)
	own.SetEnabled = NewSetPaymentTypeEnabled(own)
	return own
}

func (own *PaymentTypeManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Add, own.Edit, own.Remove, own.SetEnabled}
}

func (*PaymentTypeManage) SearchBefore(_ interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	return adminSearch(req)
}

func (own *PaymentTypeManage) DoBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	if err := adminOnly(req); err != nil {
		return nil, err, true
	}
	switch operation := sender.(type) {
	case *managepkg.Add[models.PaymentType]:
		if operation.Model == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		result, err := business.CreatePaymentType(operation.Model, strconv.FormatUint(uint64(req.NewID()), 10))
		if err == nil {
			publicapi.InvalidatePaymentTypeCache()
		}
		return result, err, true
	case *managepkg.Edit[models.PaymentType]:
		if operation.Model == nil || operation.OldItem == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		result, err := business.UpdatePaymentType(operation.OldItem.ID, operation.Model.Name, operation.Model.Code, strconv.FormatUint(uint64(req.NewID()), 10))
		if err == nil {
			publicapi.InvalidatePaymentTypeCache()
		}
		return result, err, true
	case *managepkg.Remove[models.PaymentType]:
		if operation.Model != nil {
			result, err := business.DeletePaymentType(operation.Model.ID, strconv.FormatUint(uint64(req.NewID()), 10))
			if err == nil {
				publicapi.InvalidatePaymentTypeCache()
			}
			return result, err, true
		}
	case *SetPaymentTypeEnabled:
		if operation.Model == nil {
			return nil, contract.ErrResourceNotFound, true
		}
		result, err := business.SetPaymentTypeEnabled(operation.Model.ID, operation.Model.Enabled, strconv.FormatUint(uint64(req.NewID()), 10))
		if err == nil {
			publicapi.InvalidatePaymentTypeCache()
		}
		return result, err, true
	}
	return nil, nil, false
}

func (*PaymentTypeManage) ViewModel(model *view.ViewModel) {
	model.Title, model.AutoLoad = "支付类型管理", true
}

func (*PaymentTypeManage) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	if field.IsFieldOrTitle("Enabled") {
		field.IsEdit = false
	}
}
