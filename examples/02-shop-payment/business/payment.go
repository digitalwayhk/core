package business

import (
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/02-shop-payment/models"
)

// PaymentService 处理支付创建及后台支付结果确认。
type PaymentService struct{}

// NewPaymentService 创建无状态支付业务服务。
func NewPaymentService() *PaymentService { return &PaymentService{} }

// CreatePayment 为本人未支付或支付失败订单创建新的支付尝试。
func (own *PaymentService) CreatePayment(userID string, orderID, paymentTypeID, paymentID uint) (*PaymentChange, error) {
	userID = strings.TrimSpace(userID)
	var order *models.Order
	var payment *models.PaymentRecord
	err := models.RunInTransaction(func() error {
		var err error
		order, err = models.NewOrder().FindOwned(orderID, userID)
		if err != nil {
			return err
		}
		if order == nil {
			return models.NewBusinessError("订单不存在或无权操作")
		}
		if order.Status != models.OrderStatusNormal {
			return models.NewBusinessError("当前订单不能发起支付")
		}
		if order.PaymentStatus == models.PaymentStatusPending {
			return models.NewBusinessError("订单已有支付正在处理")
		}
		if order.PaymentStatus != models.PaymentStatusUnpaid && order.PaymentStatus != models.PaymentStatusFailed {
			return models.NewBusinessError("当前订单不能发起支付")
		}
		paymentType, err := models.NewPaymentType().FindByID(paymentTypeID)
		if err != nil {
			return err
		}
		if paymentType == nil || !paymentType.Enabled {
			return models.NewBusinessError("支付类型不存在或未启用")
		}
		attempt, err := models.NewPaymentRecord().NextAttempt(order.ID)
		if err != nil {
			return err
		}
		payment = models.NewPaymentRecord()
		payment.SetID(paymentID)
		payment.OrderID = order.ID
		payment.UserID = userID
		payment.PaymentTypeID = paymentType.ID
		payment.PaymentTypeCode = paymentType.Code
		payment.PaymentTypeName = paymentType.Name
		payment.Amount = order.TotalAmount()
		payment.Attempt = attempt
		if err := payment.Insert(); err != nil {
			return err
		}
		order.PaymentID = payment.ID
		order.PaymentStatus = models.PaymentStatusPending
		return order.Update()
	})
	if err != nil {
		return nil, err
	}
	return &PaymentChange{OrderChange: OrderChange{Action: "payment_pending", Order: order}, Payment: payment}, nil
}

// ConfirmPayment 把支付中流水和订单原子更新为已支付。
func (own *PaymentService) ConfirmPayment(paymentID uint) (*PaymentChange, error) {
	return own.finishPending(paymentID, true)
}

// FailPayment 把支付中流水和订单原子更新为支付失败。
func (own *PaymentService) FailPayment(paymentID uint) (*PaymentChange, error) {
	return own.finishPending(paymentID, false)
}

// finishPending 统一支付成功和失败的原子状态迁移。
func (own *PaymentService) finishPending(paymentID uint, success bool) (*PaymentChange, error) {
	var order *models.Order
	var payment *models.PaymentRecord
	err := models.RunInTransaction(func() error {
		var err error
		payment, err = models.NewPaymentRecord().FindByID(paymentID)
		if err != nil {
			return err
		}
		if payment == nil {
			return models.NewBusinessError("支付流水不存在")
		}
		target := models.PaymentStatusFailed
		if success {
			target = models.PaymentStatusPaid
		}
		if payment.Status == target {
			order, err = models.NewOrder().FindByID(payment.OrderID)
			return err
		}
		if payment.Status != models.PaymentStatusPending {
			return models.NewBusinessError("只有支付中的流水可以处理支付结果")
		}
		order, err = models.NewOrder().FindByID(payment.OrderID)
		if err != nil {
			return err
		}
		if order == nil || order.PaymentID != payment.ID || order.PaymentStatus != models.PaymentStatusPending {
			return models.NewBusinessError("订单支付状态不一致")
		}
		payment.Status = target
		order.PaymentStatus = target
		if success {
			now := time.Now().UTC()
			payment.PaidAt = &now
		}
		if err := payment.Update(); err != nil {
			return err
		}
		return order.Update()
	})
	if err != nil {
		return nil, err
	}
	action := "payment_failed"
	if success {
		action = "paid"
	}
	return &PaymentChange{OrderChange: OrderChange{Action: action, Order: order}, Payment: payment}, nil
}

// ConfirmRefund 把退款中流水和订单原子更新为已退款、已撤销。
func (own *PaymentService) ConfirmRefund(paymentID uint) (*PaymentChange, error) {
	var order *models.Order
	var payment *models.PaymentRecord
	err := models.RunInTransaction(func() error {
		var err error
		payment, err = models.NewPaymentRecord().FindByID(paymentID)
		if err != nil {
			return err
		}
		if payment == nil {
			return models.NewBusinessError("支付流水不存在")
		}
		order, err = models.NewOrder().FindByID(payment.OrderID)
		if err != nil {
			return err
		}
		if payment.Status == models.PaymentStatusRefunded && order != nil && order.Status == models.OrderStatusCancelled {
			return nil
		}
		if order == nil || payment.Status != models.PaymentStatusRefunding || order.PaymentStatus != models.PaymentStatusRefunding {
			return models.NewBusinessError("只有退款中的流水可以确认退款")
		}
		now := time.Now().UTC()
		payment.Status = models.PaymentStatusRefunded
		payment.RefundedAt = &now
		order.Status = models.OrderStatusCancelled
		order.PaymentStatus = models.PaymentStatusRefunded
		if err := payment.Update(); err != nil {
			return err
		}
		return order.Update()
	})
	if err != nil {
		return nil, err
	}
	return &PaymentChange{OrderChange: OrderChange{Action: "cancelled", Order: order}, Payment: payment}, nil
}
