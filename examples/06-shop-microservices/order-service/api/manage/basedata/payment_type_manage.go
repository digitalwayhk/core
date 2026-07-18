// 本文件提供当前服务基础资料 Manage API 的对象管理和受控命令能力。
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

// PaymentTypeManage 定义本文件能力使用的核心结构。
type PaymentTypeManage struct {
	*BaseDataManage[models.PaymentType]
	SetEnabled *SetPaymentTypeEnabled
}

// NewPaymentTypeManage 执行本文件能力对应的业务操作。
func NewPaymentTypeManage() *PaymentTypeManage {
	own := &PaymentTypeManage{}
	own.BaseDataManage = NewBaseDataManage[models.PaymentType](own)
	own.SetEnabled = NewSetPaymentTypeEnabled(own)
	return own
}

// Routers 实现本类型在当前服务边界中的行为。
func (own *PaymentTypeManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Add, own.Edit, own.Remove, own.SetEnabled}
}

// OnAddBefore 实现本类型在当前服务边界中的行为。
func (own *PaymentTypeManage) OnAddBefore(operation *managepkg.Add[models.PaymentType], req servertypes.IRequest) (interface{}, error, bool) {
	if operation.Model == nil {
		return nil, contract.ErrResourceNotFound, true
	}
	result, err := business.CreatePaymentType(operation.Model, req.GetTraceId(), strconv.FormatUint(uint64(req.NewID()), 10))
	return result, err, true
}

// OnEditBefore 实现本类型在当前服务边界中的行为。
func (own *PaymentTypeManage) OnEditBefore(operation *managepkg.Edit[models.PaymentType], req servertypes.IRequest) (interface{}, error, bool) {
	if operation.Model == nil || operation.OldItem == nil {
		return nil, contract.ErrResourceNotFound, true
	}
	result, err := business.UpdatePaymentType(operation.OldItem.ID, operation.Model.Name, operation.Model.Code, req.GetTraceId(), strconv.FormatUint(uint64(req.NewID()), 10))
	return result, err, true
}

// OnRemoveBefore 实现本类型在当前服务边界中的行为。
func (own *PaymentTypeManage) OnRemoveBefore(operation *managepkg.Remove[models.PaymentType], req servertypes.IRequest) (interface{}, error, bool) {
	if operation.Model == nil {
		return nil, contract.ErrResourceNotFound, true
	}
	result, err := business.DeletePaymentType(operation.Model.ID, req.GetTraceId(), strconv.FormatUint(uint64(req.NewID()), 10))
	return result, err, true
}

// ViewModel 实现本类型在当前服务边界中的行为。
func (*PaymentTypeManage) ViewModel(model *view.ViewModel) {
	model.Title, model.AutoLoad = "支付类型管理", true
}

// ViewFieldModel 实现本类型在当前服务边界中的行为。
func (*PaymentTypeManage) ViewFieldModel(_ interface{}, field *view.FieldModel) {
	if field.IsFieldOrTitle("Enabled") {
		field.IsEdit = false
	}
}
