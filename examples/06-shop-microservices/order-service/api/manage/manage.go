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

func adminOnly(req servertypes.IRequest) error {
	uid, _ := req.GetUser()
	if uid != contract.PlatformAdminUserID {
		return contract.ErrForbidden
	}
	return nil
}

func adminSearch(req servertypes.IRequest) (interface{}, error, bool) {
	if err := adminOnly(req); err != nil {
		return nil, err, true
	}
	return nil, nil, false
}

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

type OrderManage struct {
	*managepkg.ManageService[models.Order]
	Cancel *CancelOrder
	Refund *RefundOrder
}

func NewOrderManage() *OrderManage {
	own := &OrderManage{}
	own.ManageService = managepkg.NewManageService[models.Order](own)
	own.Cancel, own.Refund = NewCancelOrder(own), NewRefundOrder(own)
	return own
}
func (own *OrderManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Cancel, own.Refund}
}
func (*OrderManage) SearchBefore(_ interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	return adminSearch(req)
}
func (own *OrderManage) DoBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	if err := adminOnly(req); err != nil {
		return nil, err, true
	}
	var selected *models.Order
	switch operation := sender.(type) {
	case *CancelOrder:
		selected = operation.Model
	case *RefundOrder:
		selected = operation.Model
	default:
		return nil, nil, false
	}
	if selected == nil {
		return nil, contract.ErrResourceNotFound, true
	}
	item, err := models.FindOrder(selected.ID)
	if err != nil || item == nil {
		return nil, contract.ErrResourceNotFound, true
	}
	result, err := business.CancelOrder(item.UserID, item.ID, strconv.FormatUint(uint64(req.NewID()), 10))
	return result, err, true
}
func (*OrderManage) ViewModel(model *view.ViewModel) {
	model.Title, model.AutoLoad = "订单查询", true
}

type PaymentRecordManage struct {
	*managepkg.ManageService[models.PaymentRecord]
	Confirm       *ConfirmPayment
	Fail          *FailPayment
	ConfirmRefund *ConfirmRefund
}

func NewPaymentRecordManage() *PaymentRecordManage {
	own := &PaymentRecordManage{}
	own.ManageService = managepkg.NewManageService[models.PaymentRecord](own)
	own.Confirm, own.Fail, own.ConfirmRefund = NewConfirmPayment(own), NewFailPayment(own), NewConfirmRefund(own)
	return own
}
func (own *PaymentRecordManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Confirm, own.Fail, own.ConfirmRefund}
}
func (*PaymentRecordManage) SearchBefore(_ interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	return adminSearch(req)
}
func (own *PaymentRecordManage) DoBefore(sender interface{}, req servertypes.IRequest) (interface{}, error, bool) {
	if err := adminOnly(req); err != nil {
		return nil, err, true
	}
	var paymentID string
	var action func(string, string) (interface{}, error)
	switch operation := sender.(type) {
	case *ConfirmPayment:
		if operation.Model != nil {
			paymentID = operation.Model.PaymentID
		}
		action = func(id, event string) (interface{}, error) { return business.ConfirmPayment(id, event) }
	case *FailPayment:
		if operation.Model != nil {
			paymentID = operation.Model.PaymentID
		}
		action = func(id, event string) (interface{}, error) { return business.FailPayment(id, event) }
	case *ConfirmRefund:
		if operation.Model != nil {
			paymentID = operation.Model.PaymentID
		}
		action = func(id, event string) (interface{}, error) { return business.ConfirmRefund(id, event) }
	default:
		return nil, nil, false
	}
	if paymentID == "" {
		return nil, contract.ErrResourceNotFound, true
	}
	result, err := action(paymentID, strconv.FormatUint(uint64(req.NewID()), 10))
	return result, err, true
}
func (*PaymentRecordManage) ViewModel(model *view.ViewModel) {
	model.Title, model.AutoLoad = "支付流水查询", true
}
