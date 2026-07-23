// Package transaction 提供 07 订单服务交易域 Manage 基座。
package transaction

import (
	commonmanage "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/manage/common"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// TransactionManage 是 order-service 交易域 Manage 的抽象基座。
type TransactionManage[T persistencetypes.IModel] struct {
	*commonmanage.ServiceManage[T]
}

// NewTransactionManage 创建交易域 Manage 基座。
func NewTransactionManage[T persistencetypes.IModel](owner interface{}) *TransactionManage[T] {
	return &TransactionManage[T]{ServiceManage: commonmanage.NewServiceManage[T](owner)}
}
