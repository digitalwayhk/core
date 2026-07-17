package transaction

import (
	commonmanage "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/api/manage/common"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// TransactionManage 是 supplier-service 交易/投影 Manage 的基座。
type TransactionManage[T persistencetypes.IModel] struct {
	*commonmanage.ServiceManage[T]
}

func NewTransactionManage[T persistencetypes.IModel](owner interface{}) *TransactionManage[T] {
	return &TransactionManage[T]{ServiceManage: commonmanage.NewServiceManage[T](owner)}
}
