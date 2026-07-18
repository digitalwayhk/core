// Package transaction 管理 07 订单本地可靠写入存储的进程生命周期。
package transaction

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
	"github.com/digitalwayhk/core/pkg/utils"
)

var (
	globalOrderWriteStoreMu sync.Mutex
	globalOrderWriteState   *orderWriteStoreState
	activeOrderWriteStore   atomic.Pointer[OrderWriteStore]
)

type orderWriteStoreState struct {
	path  string
	once  sync.Once
	store *OrderWriteStore
	err   error
}

// StartOrderWriteStore 启动当前 order 副本专属的 Badger 本地写入层。
func StartOrderWriteStore() error {
	globalOrderWriteStoreMu.Lock()
	defer globalOrderWriteStoreMu.Unlock()
	path := orderPendingPath()
	if globalOrderWriteState != nil && globalOrderWriteState.path != path {
		activeOrderWriteStore.Store(nil)
		if globalOrderWriteState.store != nil {
			if err := globalOrderWriteState.store.Close(10 * time.Second); err != nil {
				return fmt.Errorf("关闭旧订单写入存储失败: %w", err)
			}
		}
		globalOrderWriteState = nil
	}
	if globalOrderWriteState == nil {
		globalOrderWriteState = &orderWriteStoreState{path: path}
	}
	state := globalOrderWriteState
	state.once.Do(func() {
		config := nosql.DefaultProductionConfig(path)
		config.EnableLogger = false
		config.AutoSync = false
		state.store, state.err = newOrderWriteStore(path, config)
		if state.err == nil {
			activeOrderWriteStore.Store(state.store)
		}
	})
	return state.err
}

// StopOrderWriteStore 关闭当前 order 副本的本地写入层。
func StopOrderWriteStore() error {
	globalOrderWriteStoreMu.Lock()
	defer globalOrderWriteStoreMu.Unlock()
	if globalOrderWriteState == nil {
		return nil
	}
	activeOrderWriteStore.Store(nil)
	var err error
	if globalOrderWriteState.store != nil {
		err = globalOrderWriteState.store.Close(10 * time.Second)
	}
	globalOrderWriteState = nil
	return err
}

// GetOrderWritePerformanceSnapshot 返回当前本地写入层性能指标。
func GetOrderWritePerformanceSnapshot() (OrderWritePerformanceSnapshot, error) {
	store, err := getOrderWriteStore()
	if err != nil {
		return OrderWritePerformanceSnapshot{}, err
	}
	return store.PerformanceSnapshot(), nil
}

// AddOrder 将订单写入 Badger 本地可靠层。
func AddOrder(order *Order) error {
	store, err := getOrderWriteStore()
	if err != nil {
		return err
	}
	return store.Add(order)
}

// FindLocalOrderByRequest 按用户幂等键查询当前副本本地订单。
func FindLocalOrderByRequest(userID uint, requestID string) (*Order, error) {
	store, err := getOrderWriteStore()
	if err != nil {
		return nil, err
	}
	return store.FindPendingByRequest(userID, requestID)
}

// PendingLocalOrders 从当前副本 Badger 层读取待汇合订单。
func PendingLocalOrders(limit int) ([]*Order, error) {
	store, err := getOrderWriteStore()
	if err != nil {
		return nil, err
	}
	return store.PendingOrders(limit)
}

// RemoveLocalOrder 删除当前副本已汇合订单。
func RemoveLocalOrder(order *Order) error {
	store, err := getOrderWriteStore()
	if err != nil {
		return err
	}
	return store.RemoveLocal(order)
}

// PendingOrdersByUser 合并查询当前副本尚未同步的用户订单。
func PendingOrdersByUser(userID uint) ([]*Order, error) {
	store, err := getOrderWriteStore()
	if err != nil {
		return nil, err
	}
	return store.PendingByUser(userID)
}

func getOrderWriteStore() (*OrderWriteStore, error) {
	if store := activeOrderWriteStore.Load(); store != nil {
		return store, nil
	}
	if err := StartOrderWriteStore(); err != nil {
		return nil, err
	}
	if store := activeOrderWriteStore.Load(); store != nil {
		return store, nil
	}
	globalOrderWriteStoreMu.Lock()
	defer globalOrderWriteStoreMu.Unlock()
	if globalOrderWriteState == nil || globalOrderWriteState.store == nil {
		return nil, errors.New("订单写入存储未初始化")
	}
	return globalOrderWriteState.store, nil
}

func orderPendingPath() string {
	if path := os.Getenv("SHOP_LOCAL_PENDING_DIR"); path != "" {
		return path
	}
	return filepath.Join(utils.Getpath(), "data", "order-scale-pending")
}
