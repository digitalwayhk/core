// 本文件提供当前服务基础资料 Manage API 的对象管理和受控命令能力。
package basedata

import (
	commonmanage "github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/api/manage/common"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// BaseDataManage 是 order-service 基础资料 Manage 的基座。
type BaseDataManage[T persistencetypes.IModel] struct {
	*commonmanage.ServiceManage[T]
}

// NewBaseDataManage 执行本文件能力对应的业务操作。
func NewBaseDataManage[T persistencetypes.IModel](owner interface{}) *BaseDataManage[T] {
	return &BaseDataManage[T]{ServiceManage: commonmanage.NewServiceManage[T](owner)}
}
