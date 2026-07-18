// Package business 提供 07 订单服务支付类型业务能力。
package business

import "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"

// ListPaymentTypes 读取共享远程权威库中的支付类型。
func ListPaymentTypes(enabledOnly bool) ([]*models.PaymentType, error) {
	var items []*models.PaymentType
	err := models.RunRemoteTransaction(func(action models.DataAction) error {
		var err error
		items, err = models.ListPaymentTypesWith(action, enabledOnly)
		return err
	})
	return items, err
}

// SavePaymentType 保存支付类型配置。
func SavePaymentType(item *models.PaymentType) (*models.PaymentType, error) {
	err := models.RunRemoteTransaction(func(action models.DataAction) error {
		return models.SavePaymentTypeWith(action, item)
	})
	return item, err
}
