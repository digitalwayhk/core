package common

import (
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
)

// ServiceManage 是 supplier-service 全部 Manage 的服务级基座。
// owner 必须传最终具体 Manage，确保 Hook 分派到最末层实现。
type ServiceManage[T persistencetypes.IModel] struct {
	*managepkg.HookedManageService[T]
}

func NewServiceManage[T persistencetypes.IModel](owner interface{}) *ServiceManage[T] {
	return &ServiceManage[T]{HookedManageService: managepkg.NewHookedManageService[T](owner)}
}
