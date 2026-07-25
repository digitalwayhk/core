// Package manage 是 Order Service 后台管理 API 的兼容门面。
package manage

import (
	basedatamanage "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/manage/basedata"
	transactionmanage "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/manage/transaction"
)

type (
	PaymentTypeManage     = basedatamanage.PaymentTypeManage
	SetPaymentTypeEnabled = basedatamanage.SetPaymentTypeEnabled
	OrderManage           = transactionmanage.OrderManage
	PaymentRecordManage   = transactionmanage.PaymentRecordManage
	CancelOrder           = transactionmanage.CancelOrder
	RefundOrder           = transactionmanage.RefundOrder
	ConfirmPayment        = transactionmanage.ConfirmPayment
	FailPayment           = transactionmanage.FailPayment
	ConfirmRefund         = transactionmanage.ConfirmRefund
)

var (
	NewPaymentTypeManage     = basedatamanage.NewPaymentTypeManage
	NewSetPaymentTypeEnabled = basedatamanage.NewSetPaymentTypeEnabled
	NewOrderManage           = transactionmanage.NewOrderManage
	NewPaymentRecordManage   = transactionmanage.NewPaymentRecordManage
	NewCancelOrder           = transactionmanage.NewCancelOrder
	NewRefundOrder           = transactionmanage.NewRefundOrder
	NewConfirmPayment        = transactionmanage.NewConfirmPayment
	NewFailPayment           = transactionmanage.NewFailPayment
	NewConfirmRefund         = transactionmanage.NewConfirmRefund
)
