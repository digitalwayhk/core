package models

import (
	"errors"

	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// requireDataAction 拒绝缺失的数据适配器，避免模型静默回退到进程级具体数据库。
func requireDataAction(action persistencetypes.IDataAction) error {
	if action == nil {
		return errors.New("模型持久化需要显式配置 IDataAction")
	}
	return nil
}
