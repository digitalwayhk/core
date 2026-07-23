// Package basedata 提供 07 订单服务基础资料 Manage 基座。
package basedata

import (
	commonmanage "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/manage/common"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// BaseDataManage 是 order-service 基础资料 Manage 的抽象基座。
type BaseDataManage[T persistencetypes.IModel] struct {
	*commonmanage.ServiceManage[T]
}

// NewBaseDataManage 创建基础资料 Manage 基座。
func NewBaseDataManage[T persistencetypes.IModel](owner interface{}) *BaseDataManage[T] {
	return &BaseDataManage[T]{ServiceManage: commonmanage.NewServiceManage[T](owner)}
}
