package basedata

import (
	"strconv"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/business"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

type PaymentTypeManage struct {
	*BaseDataManage[models.PaymentType]
	SetEnabled *SetPaymentTypeEnabled
}

func NewPaymentTypeManage() *PaymentTypeManage {
	own := &PaymentTypeManage{}
	own.BaseDataManage = NewBaseDataManage[models.PaymentType](own)
	own.SetEnabled = NewSetPaymentTypeEnabled(own)
	return own
}

func (own *PaymentTypeManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Add, own.Edit, own.Remove, own.SetEnabled}
}

func (own *PaymentTypeManage) OnAddBefore(operation *managepkg.Add[models.PaymentType], req servertypes.IRequest) (interface{}, error, bool) {
	if operation.Model == nil {
		return nil, contract.ErrResourceNotFound, true
	}
	result, err := business.CreatePaymentType(operation.Model, strconv.FormatUint(uint64(req.NewID()), 10))
	return result, err, true
}

func (own *PaymentTypeManage) OnEditBefore(operation *managepkg.Edit[models.PaymentType], req servertypes.IRequest) (interface{}, error, bool) {
	if operation.Model == nil || operation.OldItem == nil {
		return nil, contract.ErrResourceNotFound, true
	}
	result, err := business.UpdatePaymentType(operation.OldItem.ID, operation.Model.Name, operation.Model.Code, strconv.FormatUint(uint64(req.NewID()), 10))
	return result, err, true
}

func (own *PaymentTypeManage) OnRemoveBefore(operation *managepkg.Remove[models.PaymentType], req servertypes.IRequest) (interface{}, error, bool) {
	if operation.Model == nil {
		return nil, contract.ErrResourceNotFound, true
	}
	result, err := business.DeletePaymentType(operation.Model.ID, strconv.FormatUint(uint64(req.NewID()), 10))
	return result, err, true
}

func (*PaymentTypeManage) ViewModel(model *view.ViewModel) {
	model.Title, model.AutoLoad = "支付类型管理", true
}

func (*PaymentTypeManage) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	if field.IsFieldOrTitle("Enabled") {
		field.IsEdit = false
	}
}
