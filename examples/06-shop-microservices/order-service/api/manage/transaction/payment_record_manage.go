package transaction

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage/view"
)

type PaymentRecordManage struct {
	*TransactionManage[models.PaymentRecord]
	Confirm       *ConfirmPayment
	Fail          *FailPayment
	ConfirmRefund *ConfirmRefund
}

func NewPaymentRecordManage() *PaymentRecordManage {
	own := &PaymentRecordManage{}
	own.TransactionManage = NewTransactionManage[models.PaymentRecord](own)
	own.Confirm, own.Fail, own.ConfirmRefund = NewConfirmPayment(own), NewFailPayment(own), NewConfirmRefund(own)
	return own
}

func (own *PaymentRecordManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Confirm, own.Fail, own.ConfirmRefund}
}

func handlePaymentCommand[T any](model *models.PaymentRecord, traceID, eventID string, action func(string, string, string) (T, error)) (interface{}, error) {
	if model == nil || model.PaymentID == "" {
		return nil, contract.ErrResourceNotFound
	}
	return action(model.PaymentID, traceID, eventID)
}

func (*PaymentRecordManage) ViewModel(model *view.ViewModel) {
	model.Title, model.AutoLoad = "支付流水查询", true
}
