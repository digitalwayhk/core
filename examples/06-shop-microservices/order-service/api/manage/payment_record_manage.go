package manage

import (
	"strconv"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/business"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

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
