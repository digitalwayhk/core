package basedata

import (
	commonmanage "github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/api/manage/common"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// BaseDataManage 是 user-service 基础资料 Manage 的基座。
type BaseDataManage[T persistencetypes.IModel] struct {
	*commonmanage.ServiceManage[T]
}

func NewBaseDataManage[T persistencetypes.IModel](owner interface{}) *BaseDataManage[T] {
	return &BaseDataManage[T]{ServiceManage: commonmanage.NewServiceManage[T](owner)}
}
