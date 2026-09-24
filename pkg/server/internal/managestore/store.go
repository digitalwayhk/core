// Package managestore 保存 Core 系统 Manage 模型的进程级控制面数据源。
package managestore

import (
	"errors"
	"sync"

	persistencetype "github.com/digitalwayhk/core/pkg/persistence/types"
)

var configured struct {
	sync.RWMutex
	action persistencetype.IDataAction
	key    string
}

// Configure 由 WebServer 控制面启动流程绑定唯一数据源。
func Configure(action persistencetype.IDataAction) error {
	if action == nil {
		return errors.New("manage control-plane action is required")
	}
	keyed, ok := action.(interface{ GetSyncPoolKey(interface{}) string })
	if !ok {
		return errors.New("manage control-plane action has no stable store identity")
	}
	key := keyed.GetSyncPoolKey(nil)
	if key == "" {
		return errors.New("manage control-plane store identity is empty")
	}
	configured.Lock()
	defer configured.Unlock()
	if configured.action != nil {
		if configured.key != key {
			return errors.New("manage control-plane store is already configured")
		}
		return nil
	}
	configured.action, configured.key = action, key
	return nil
}

// Clone 返回共享连接池但不共享事务状态的数据操作器。
func Clone() persistencetype.IDataAction {
	configured.RLock()
	action := configured.action
	configured.RUnlock()
	if cloner, ok := action.(interface {
		Clone() persistencetype.IDataAction
	}); ok {
		return cloner.Clone()
	}
	return action
}

// ResetForTesting 清除进程级绑定，只供相互隔离的测试用例使用。
func ResetForTesting() {
	configured.Lock()
	configured.action, configured.key = nil, ""
	configured.Unlock()
}
