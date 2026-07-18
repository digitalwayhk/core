// Package store 封装 07 订单服务共享远程权威库的数据访问能力。
package store

import (
	"sync"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/common"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

var (
	remoteActionOnce sync.Once
	remoteAction     persistencetypes.IDataAction
	remoteTxMu       sync.Mutex
	remoteEnsureMu   sync.Mutex
)

// GetRemote 返回所有 order 实例共享远程权威库的数据访问器。
func GetRemote() persistencetypes.IDataAction {
	remoteActionOnce.Do(func() { remoteAction = entity.GetGlobalSqliteInstance(common.RemoteDatabaseName) })
	return remoteAction
}

// EnsureRemoteModel 确保远程权威模型表已创建。
func EnsureRemoteModel(model interface{}) error {
	return ensureModelWith(GetRemote(), &remoteEnsureMu, model)
}

// RunRemoteTransaction 在共享远程权威库中串行执行事务。
func RunRemoteTransaction(ensureStorage func() error, operation func(persistencetypes.IDataAction) error) error {
	return runTransaction(&remoteTxMu, &remoteEnsureMu, GetRemote, ensureStorage, operation)
}
