// 本文件提供当前服务交易域 Manage API 的查询、状态命令和受控操作能力。
package transaction

import (
	commonmanage "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/manage/common"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// TransactionManage 是 order-service 交易/投影 Manage 的基座。
type TransactionManage[T persistencetypes.IModel] struct {
	*commonmanage.ServiceManage[T]
}

// NewTransactionManage 执行本文件能力对应的业务操作。
func NewTransactionManage[T persistencetypes.IModel](owner interface{}) *TransactionManage[T] {
	return &TransactionManage[T]{ServiceManage: commonmanage.NewServiceManage[T](owner)}
}
