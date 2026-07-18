// 本文件提供当前服务交易域 Manage API 的查询、状态命令和受控操作能力。
package transaction

import (
	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage/view"
)

// PaymentRecordManage 定义本文件能力使用的核心结构。
type PaymentRecordManage struct {
	*TransactionManage[models.PaymentRecord]
	Confirm       *ConfirmPayment
	Fail          *FailPayment
	ConfirmRefund *ConfirmRefund
}

// NewPaymentRecordManage 执行本文件能力对应的业务操作。
func NewPaymentRecordManage() *PaymentRecordManage {
	own := &PaymentRecordManage{}
	own.TransactionManage = NewTransactionManage[models.PaymentRecord](own)
	own.Confirm, own.Fail, own.ConfirmRefund = NewConfirmPayment(own), NewFailPayment(own), NewConfirmRefund(own)
	return own
}

// Routers 实现本类型在当前服务边界中的行为。
func (own *PaymentRecordManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{own.View, own.Search, own.Confirm, own.Fail, own.ConfirmRefund}
}

func handlePaymentCommand[T any](model *models.PaymentRecord, traceID, eventID string, action func(string, string, string) (T, error)) (interface{}, error) {
	if model == nil || model.PaymentID == "" {
		return nil, contract.ErrResourceNotFound
	}
	return action(model.PaymentID, traceID, eventID)
}

// ViewModel 实现本类型在当前服务边界中的行为。
func (*PaymentRecordManage) ViewModel(model *view.ViewModel) {
	model.Title, model.AutoLoad = "支付流水查询", true
}
