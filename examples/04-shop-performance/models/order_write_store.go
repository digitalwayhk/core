package models

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

// OrderWriteStore 使用 Badger 接收订单写入，再复用框架写后同步能力汇合到 SQLite。
type OrderWriteStore struct {
	db      *nosql.PrefixedBadgerDB[Order]
	addMu   sync.Mutex
	flushMu sync.Mutex
	recent  map[string]time.Time
	closeMu sync.Mutex
	closed  bool
}

func newOrderWriteStore(path string, action persistencetypes.IDataAction, config nosql.BadgerDBConfig) (*OrderWriteStore, error) {
	db, err := nosql.NewSharedBadgerDB[Order](path, config)
	if err != nil {
		return nil, err
	}
	list := entity.NewModelList[Order](action)
	if err := db.EnableWriteBehind(list); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &OrderWriteStore{db: db, recent: make(map[string]time.Time)}, nil
}

// Add 将订单持久写入本地 Badger；返回成功后请求即可完成。
func (s *OrderWriteStore) Add(order *Order) error {
	if order == nil {
		return NewValidationError("订单不能为空")
	}
	order.prepareForInsert()
	key := order.GetHash()
	if key == "" {
		return NewValidationError("订单缓存键无效")
	}

	s.addMu.Lock()
	defer s.addMu.Unlock()
	now := time.Now().UTC()
	for candidate, expiresAt := range s.recent {
		if !expiresAt.After(now) {
			delete(s.recent, candidate)
		}
	}
	if _, exists := s.recent[key]; exists {
		return NewBusinessError("同一用户每秒只能购买一次同一商品")
	}
	if _, err := s.db.Get(key); err == nil {
		return NewBusinessError("同一用户每秒只能购买一次同一商品")
	} else if !errors.Is(err, badger.ErrKeyNotFound) {
		return err
	}
	if err := s.db.Set(order, 0); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return NewBusinessError("同一用户每秒只能购买一次同一商品")
		}
		return err
	}
	s.recent[key] = order.CreatedAt.UTC().Truncate(time.Second).Add(time.Second)
	return nil
}

// PendingByUser 返回尚在本地层可见的用户订单。
func (s *OrderWriteStore) PendingByUser(userID string) ([]*Order, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, NewBusinessError("用户身份无效")
	}
	items, err := s.db.Scan(orderUserPrefix(userID), 0)
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID > items[j].ID })
	return items, nil
}

// FindPendingOwned 从本地层查找指定用户的订单。
func (s *OrderWriteStore) FindPendingOwned(userID string, orderID uint) (*Order, error) {
	items, err := s.PendingByUser(userID)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item != nil && item.ID == orderID {
			return item, nil
		}
	}
	return nil, nil
}

// RemoveLocal 删除已由 SQLite 状态机处理完成的本地副本，不产生远端删除操作。
func (s *OrderWriteStore) RemoveLocal(order *Order) error {
	err := s.db.DeleteByItemWithSync(order, false)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil
	}
	return err
}

// Flush 串行等待当前待同步订单写入 SQLite，避免多个事务入口重复冲刷同一批数据。
func (s *OrderWriteStore) Flush() error {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	return s.db.ForceSyncAll()
}

// SyncStatus 返回积压、同步进度和关闭状态。
func (s *OrderWriteStore) SyncStatus() nosql.SyncStatus { return s.db.GetSyncStatus() }

// Close 先尽力汇合积压，再关闭 Badger 和后台同步协程。
func (s *OrderWriteStore) Close(timeout time.Duration) error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	flushErr := s.Flush()
	closeErr := s.db.CloseWithTimeout(timeout, timeout)
	return errors.Join(flushErr, closeErr)
}

var (
	globalOrderWriteStoreMu sync.Mutex
	globalOrderWriteState   *orderWriteStoreState
)

type orderWriteStoreState struct {
	path  string
	once  sync.Once
	store *OrderWriteStore
	err   error
}

// StartOrderWriteStore 启动当前服务进程专属的订单写后同步存储。
func StartOrderWriteStore() error {
	globalOrderWriteStoreMu.Lock()
	defer globalOrderWriteStoreMu.Unlock()
	path := filepath.Join(utils.Getpath(), "data", "order-write-behind")
	if globalOrderWriteState != nil && globalOrderWriteState.path != path {
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
	// 初始化错误由同一生命周期内的所有请求稳定观察；只有 Stop 才重置状态。
	state.once.Do(func() {
		if err := EnsureStorage(); err != nil {
			state.err = err
			return
		}
		config := nosql.DefaultProductionConfig(path)
		config.EnableLogger = false
		config.AutoSync = true
		config.SyncBatchDelay = 500 * time.Millisecond
		state.store, state.err = newOrderWriteStore(path, cloneDataAction(), config)
	})
	return state.err
}

func getOrderWriteStore() (*OrderWriteStore, error) {
	if err := StartOrderWriteStore(); err != nil {
		return nil, err
	}
	globalOrderWriteStoreMu.Lock()
	defer globalOrderWriteStoreMu.Unlock()
	return globalOrderWriteState.store, nil
}

// StopOrderWriteStore 汇合积压并释放当前服务的本地订单存储。
func StopOrderWriteStore() error {
	globalOrderWriteStoreMu.Lock()
	defer globalOrderWriteStoreMu.Unlock()
	if globalOrderWriteState == nil {
		return nil
	}
	var err error
	if globalOrderWriteState.store != nil {
		err = globalOrderWriteState.store.Close(10 * time.Second)
	}
	globalOrderWriteState = nil
	return err
}

// QueryVisibleOrders 合并 SQLite 已同步订单与 Badger 待同步订单，并按 ID 倒序去重。
func QueryVisibleOrders(userID string) ([]*Order, error) {
	persisted, err := NewOrder().QueryByUser(userID)
	if err != nil {
		return nil, err
	}
	store, err := getOrderWriteStore()
	if err != nil {
		return nil, err
	}
	pending, err := store.PendingByUser(userID)
	if err != nil {
		return nil, err
	}
	byID := make(map[uint]*Order, len(persisted)+len(pending))
	for _, order := range persisted {
		if order != nil {
			byID[order.ID] = order
		}
	}
	for _, order := range pending {
		if order != nil {
			byID[order.ID] = order
		}
	}
	result := make([]*Order, 0, len(byID))
	for _, order := range byID {
		result = append(result, order)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID > result[j].ID })
	return result, nil
}

// FlushPendingOrder 在需要 SQLite 事务处理前汇合指定本地订单。
func FlushPendingOrder(userID string, orderID uint) error {
	persisted, err := NewOrder().FindOwned(orderID, userID)
	if err != nil {
		return err
	}
	if persisted != nil {
		return nil
	}
	store, err := getOrderWriteStore()
	if err != nil {
		return err
	}
	pending, err := store.FindPendingOwned(userID, orderID)
	if err != nil || pending == nil {
		return err
	}
	return store.Flush()
}

// FlushOrders 汇合全部待同步订单，供引用完整性检查和服务关闭使用。
func FlushOrders() error {
	store, err := getOrderWriteStore()
	if err != nil {
		return err
	}
	return store.Flush()
}
