package nosql

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/json"

	"github.com/dgraph-io/badger/v3"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/zeromicro/go-zero/core/logx"
)

// ScanResult 分页扫描结果
type ScanResult[T types.IModel] struct {
	Items   []*T   `json:"items"`    // 数据列表
	LastKey string `json:"last_key"` // 最后一个 key（用于下次分页）
	HasMore bool   `json:"has_more"` // 是否还有更多数据
}
type OpType string

const (
	OpInsert OpType = "insert"
	OpUpdate OpType = "update"
	OpDelete OpType = "delete" // 🔧 删除操作
)

// SyncQueueItem 同步队列项（包装数据）
type SyncQueueItem[T types.IModel] struct {
	Key       string    `json:"key"`
	Item      *T        `json:"item,omitempty"`
	Op        OpType    `json:"op"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	IsSynced  bool      `json:"is_synced"`
	SyncedAt  time.Time `json:"synced_at,omitempty"`
	IsDeleted bool      `json:"is_deleted"`
	DeletedAt time.Time `json:"deleted_at,omitempty"`
}

// PrefixedBadgerDB 带前缀的共享 BadgerDB
type PrefixedBadgerDB[T types.IModel] struct {
	manager *SharedBadgerManager
	prefix  string // "user:", "order:", "product:"

	syncDB         bool
	syncList       *entity.ModelList[T]
	syncLock       sync.RWMutex
	syncMutex      sync.Mutex
	syncInProgress bool
	closeCh        chan struct{}
	wg             sync.WaitGroup
	syncOnce       sync.Once
	isAutoClean    bool

	// 待同步计数缓存
	pendingCountCache int
	pendingCountMutex sync.RWMutex
	lastCountUpdate   time.Time

	// ✅ 关闭状态控制
	closeOnce sync.Once
	closed    bool
	closeMu   sync.RWMutex
}

// NewSharedBadgerDB 创建共享 BadgerDB 实例
func NewSharedBadgerDB[T types.IModel](basePath string, config ...BadgerDBConfig) (*PrefixedBadgerDB[T], error) {
	prefix := reflect.TypeOf((*T)(nil)).Elem().Name() + ":"
	manager, err := GetSharedManager(basePath, config...)
	if err != nil {
		return nil, err
	}

	manager.AddRef(prefix)

	db := &PrefixedBadgerDB[T]{
		manager: manager,
		prefix:  prefix,
		closeCh: make(chan struct{}),
	}

	logx.Infof("共享BadgerDB实例已创建 [prefix=%s]", prefix)
	return db, nil
}

// generateKey 生成带前缀的 key
func (p *PrefixedBadgerDB[T]) generateKey(item *T) string {
	if item == nil {
		return ""
	}
	if rowCode, ok := any(item).(types.IRowCode); ok {
		return p.prefix + rowCode.GetHash()
	}
	return ""
}

// SetSyncDB 设置同步数据库
func (p *PrefixedBadgerDB[T]) SetSyncDB(list *entity.ModelList[T]) {
	p.syncLock.Lock()
	defer p.syncLock.Unlock()

	if list != nil {
		if p.syncDB {
			return
		}
		p.syncDB = true
	} else {
		if !p.syncDB {
			return
		}
		p.syncDB = false
	}

	p.syncList = list

	if list != nil && p.syncDB {
		p.syncOnce.Do(func() {
			p.wg.Add(1)
			go p.syncToOtherDB()
			logx.Infof("共享DB自动同步已启动 [prefix=%s]", p.prefix)
		})
	}
}

// Set 写入数据
func (p *PrefixedBadgerDB[T]) Set(item *T, ttl time.Duration, fn ...func(wrapper *SyncQueueItem[T])) error {
	if p.IsClosed() {
		return fmt.Errorf("数据库实例已关闭 [prefix=%s]", p.prefix)
	}
	key := p.generateKey(item)
	if key == "" {
		return badger.ErrEmptyKey
	}

	p.syncLock.RLock()
	needSync := p.syncDB
	p.syncLock.RUnlock()

	data, err := p.setItem(key, needSync, item, fn...)
	if err != nil {
		return err
	}

	err = p.manager.db.Update(func(txn *badger.Txn) error {
		entry := badger.NewEntry([]byte(key), data)
		if ttl > 0 {
			entry = entry.WithTTL(ttl)
		}
		return txn.SetEntry(entry)
	})

	if err == nil && needSync {
		p.incrementPendingCount(1)
	}
	return err
}
func (p *PrefixedBadgerDB[T]) BatchInsert(items []*T) error {
	if p.IsClosed() {
		return fmt.Errorf("数据库实例已关闭 [prefix=%s]", p.prefix)
	}
	if len(items) == 0 {
		return nil
	}

	p.syncLock.RLock()
	needSync := p.syncDB
	p.syncLock.RUnlock()

	err := p.manager.db.Update(func(txn *badger.Txn) error {
		for _, item := range items {
			key := p.generateKey(item)
			if key == "" {
				return badger.ErrEmptyKey
			}

			data, err := p.setItem(key, needSync, item)
			if err != nil {
				return err
			}

			entry := badger.NewEntry([]byte(key), data)
			if err := txn.SetEntry(entry); err != nil {
				return err
			}
		}
		return nil
	})

	if err == nil && needSync {
		p.incrementPendingCount(len(items))
	}
	return err
}

// setItem 内部方法（复用原有逻辑）
func (p *PrefixedBadgerDB[T]) setItem(key string, needSync bool, item *T, fn ...func(wrapper *SyncQueueItem[T])) ([]byte, error) {
	if item == nil {
		return nil, fmt.Errorf("item 不能为空")
	}

	existingWrapper, err := p.getWrapper(key)
	var wrapper *SyncQueueItem[T]

	if err == nil && existingWrapper != nil {
		if existingWrapper.IsDeleted {
			return nil, fmt.Errorf("无法更新已删除的项，key=%s", key)
		}
		wrapper = existingWrapper
		wrapper.Op = OpUpdate
		wrapper.Item = item
		wrapper.UpdatedAt = time.Now()
		wrapper.IsSynced = !needSync
		if irde, ok := any(item).(types.IRowDate); ok {
			irde.SetUpdatedAt(wrapper.UpdatedAt)
		}
	} else {
		now := time.Now()
		wrapper = &SyncQueueItem[T]{
			Key:       key,
			Item:      item,
			Op:        OpInsert,
			CreatedAt: now,
			UpdatedAt: now,
			IsSynced:  !needSync,
			IsDeleted: false,
		}
		if irde, ok := any(item).(types.IRowDate); ok {
			irde.SetCreatedAt(now)
			irde.SetUpdatedAt(now)
		}
	}

	data, err := json.Marshal(wrapper)
	if err != nil {
		return nil, fmt.Errorf("序列化失败: %w", err)
	}

	if len(fn) > 0 {
		fn[0](wrapper)
	}
	return data, nil
}

// Get 获取数据
func (p *PrefixedBadgerDB[T]) Get(key string) (*T, error) {
	if p.IsClosed() {
		return nil, fmt.Errorf("数据库实例已关闭 [prefix=%s]", p.prefix)
	}
	fullKey := p.prefix + key
	wrapper, err := p.getWrapper(fullKey)
	if err != nil {
		return nil, err
	}

	if wrapper.IsDeleted {
		return nil, badger.ErrKeyNotFound
	}

	return wrapper.Item, nil
}

// getWrapper 内部方法
func (p *PrefixedBadgerDB[T]) getWrapper(key string) (*SyncQueueItem[T], error) {
	var wrapper = new(SyncQueueItem[T])

	err := p.manager.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, wrapper)
		})
	})

	if err != nil {
		return nil, err
	}

	if wrapper.Item != nil {
		if hook, ok := any(wrapper.Item).(types.IModelNewHook); ok {
			hook.NewModel()
		}
	}

	return wrapper, nil
}

// Delete 删除数据
func (p *PrefixedBadgerDB[T]) Delete(key string) error {
	if p.IsClosed() {
		return fmt.Errorf("数据库实例已关闭 [prefix=%s]", p.prefix)
	}
	fullKey := p.prefix + key

	p.syncLock.RLock()
	needSync := p.syncDB
	p.syncLock.RUnlock()

	return p.delete(fullKey, needSync)
}
func (p *PrefixedBadgerDB[T]) DeleteByItem(item *T) error {
	key := p.generateKey(item)
	if key == "" {
		return badger.ErrEmptyKey
	}

	p.syncLock.RLock()
	needSync := p.syncDB
	p.syncLock.RUnlock()

	return p.delete(key, needSync)
}
func (p *PrefixedBadgerDB[T]) DeleteByItemWithSync(item *T, needSync bool) error {
	key := p.generateKey(item)
	if key == "" {
		return badger.ErrEmptyKey
	}

	return p.delete(key, needSync)
}
func (p *PrefixedBadgerDB[T]) batchDelete(keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	return p.manager.db.Update(func(txn *badger.Txn) error {
		for _, key := range keys {
			if err := txn.Delete([]byte(key)); err != nil {
				return err
			}
		}
		return nil
	})
}

// delete 内部方法
func (p *PrefixedBadgerDB[T]) delete(key string, needSync bool) error {
	if !needSync {
		return p.manager.db.Update(func(txn *badger.Txn) error {
			return txn.Delete([]byte(key))
		})
	}

	if !p.syncDB {
		return fmt.Errorf("未启用同步数据库功能，无法执行软删除")
	}

	return p.manager.db.Update(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return nil
			}
			return err
		}

		var wrapper SyncQueueItem[T]
		err = item.Value(func(val []byte) error {
			return json.Unmarshal(val, &wrapper)
		})
		if err != nil {
			return err
		}

		if wrapper.IsDeleted {
			return nil
		}

		now := time.Now()
		wrapper.Op = OpDelete
		wrapper.IsDeleted = true
		wrapper.DeletedAt = now
		wrapper.UpdatedAt = now
		wrapper.IsSynced = false

		data, err := json.Marshal(&wrapper)
		if err != nil {
			return fmt.Errorf("序列化失败: %w", err)
		}

		return txn.Set([]byte(key), data)
	})
}

// Scan 扫描数据（仅扫描当前前缀）
func (p *PrefixedBadgerDB[T]) Scan(prefix string, limit int) ([]*T, error) {
	if p.IsClosed() {
		return nil, fmt.Errorf("数据库实例已关闭 [prefix=%s]", p.prefix)
	}
	var results []*T
	prefix = p.prefix + prefix
	err := p.manager.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 1000 // 增加预取大小
		opts.PrefetchValues = true
		opts.Reverse = false
		opts.AllVersions = false // 只读取最新版本
		it := txn.NewIterator(opts)
		defer it.Close()

		count := 0
		for it.Seek([]byte(prefix)); it.ValidForPrefix([]byte(prefix)); it.Next() {
			if limit > 0 && count >= limit {
				break
			}

			item := it.Item()

			err := item.Value(func(val []byte) error {
				var wrapper SyncQueueItem[T]
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return err
				}

				if wrapper.IsDeleted {
					return nil
				}

				if wrapper.Item != nil {
					if hook, ok := any(wrapper.Item).(types.IModelNewHook); ok {
						hook.NewModel()
					}
					results = append(results, wrapper.Item)
					count++
				}
				return nil
			})

			if err != nil {
				logx.Errorf("解析数据失败: %v", err)
				continue
			}
		}
		return nil
	})

	return results, err
}
func (p *PrefixedBadgerDB[T]) ScanAll() ([]*T, error) {
	var results []*T
	err := p.manager.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 100
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Seek([]byte(p.prefix)); it.ValidForPrefix([]byte(p.prefix)); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var wrapper SyncQueueItem[T]
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return err
				}
				if wrapper.IsDeleted {
					return nil
				}
				if wrapper.Item != nil {
					if hook, ok := any(wrapper.Item).(types.IModelNewHook); ok {
						hook.NewModel()
					}
					results = append(results, wrapper.Item)
				}
				return nil
			})
			if err != nil {
				logx.Errorf("解析数据失败: %v", err)
				continue
			}
		}
		return nil
	})
	return results, err
}
func (p *PrefixedBadgerDB[T]) ScanPage(prefix string, limit int, lastKey string) (*ScanResult[T], error) {
	prefix = p.prefix + prefix
	if limit <= 0 {
		limit = 1000 // 默认每页 1000 条
	}

	result := &ScanResult[T]{
		Items: make([]*T, 0, limit),
	}

	err := p.manager.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 100
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		// 确定起始位置
		var startKey []byte
		if lastKey != "" {
			startKey = []byte(lastKey)
		} else {
			startKey = []byte(prefix)
		}

		count := 0
		firstItem := true

		for it.Seek(startKey); it.ValidForPrefix([]byte(prefix)); it.Next() {
			// 跳过上一页的最后一条（避免重复）
			if lastKey != "" && firstItem {
				currentKey := string(it.Item().Key())
				if currentKey == lastKey {
					firstItem = false
					continue
				}
			}
			firstItem = false

			// 达到限制后再读一条，判断是否还有更多数据
			if count >= limit {
				result.HasMore = true
				break
			}

			item := it.Item()
			currentKey := string(item.Key())

			err := item.Value(func(val []byte) error {
				var wrapper SyncQueueItem[T]
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return err
				}

				// 过滤已删除的数据
				if wrapper.IsDeleted {
					return nil
				}

				if wrapper.Item != nil {
					if hook, ok := any(wrapper.Item).(types.IModelNewHook); ok {
						hook.NewModel()
					}
					result.Items = append(result.Items, wrapper.Item)
					result.LastKey = currentKey
					count++
				}
				return nil
			})

			if err != nil {
				logx.Errorf("解析数据失败: %v", err)
				continue
			}
		}

		return nil
	})

	return result, err
}

// incrementPendingCount 更新待同步计数
func (p *PrefixedBadgerDB[T]) incrementPendingCount(delta int) {
	p.pendingCountMutex.Lock()
	p.pendingCountCache += delta
	p.pendingCountMutex.Unlock()
}

// getDataAction 获取同步操作
func (p *PrefixedBadgerDB[T]) getDataAction(item *T) types.IDataAction {
	if p.syncList != nil {
		model := item
		if model == nil {
			model = new(T)
			if nm, ok := any(model).(types.IModelNewHook); ok {
				nm.NewModel()
			}
		}
		searchItem := p.syncList.GetSearchItem()
		searchItem.Model = model
		action := p.syncList.GetDBAdapter(searchItem)
		return action
	}
	return nil
}

// syncToOtherDB 同步到其他数据库（修复死循环问题 + 立即同步）
func (p *PrefixedBadgerDB[T]) syncToOtherDB() {
	defer p.wg.Done()

	config := p.manager.config
	interval := config.SyncInterval
	minInterval := config.SyncMinInterval
	maxInterval := config.SyncMaxInterval

	// 🆕 启动后立即检查一次
	p.syncLock.RLock()
	hasDB := p.syncDB
	p.syncLock.RUnlock()

	if hasDB {
		pendingCount, err := p.GetPendingSyncCount()
		if err == nil && pendingCount > 0 {
			logx.Infof("启动时发现待同步数据 [prefix=%s, count=%d], 立即执行同步", p.prefix, pendingCount)

			p.syncMutex.Lock()
			p.syncInProgress = true
			p.syncMutex.Unlock()

			start := time.Now()
			if err := p.processSyncQueue(); err != nil {
				logx.Errorf("初始同步失败 [prefix=%s]: %v", p.prefix, err)
			}
			duration := time.Since(start)

			remainingCount, _ := p.GetPendingSyncCount()

			p.syncMutex.Lock()
			p.syncInProgress = false
			p.syncMutex.Unlock()

			logx.Infof("初始同步完成 [prefix=%s, 处理: %d, 剩余: %d, 耗时: %v]",
				p.prefix, pendingCount, remainingCount, duration)

			// 🆕 如果还有剩余数据,缩短间隔
			if remainingCount > 0 {
				interval = minInterval
			}
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.syncLock.RLock()
			hasDB := p.syncDB
			p.syncLock.RUnlock()

			if !hasDB {
				continue
			}

			pendingCount, err := p.GetPendingSyncCount()
			if err != nil {
				logx.Errorf("获取待同步数量失败 [prefix=%s]: %v", p.prefix, err)
				interval = min(interval*2, maxInterval)
				ticker.Reset(interval)
				continue
			}

			if pendingCount == 0 {
				interval = min(interval*2, maxInterval)
				ticker.Reset(interval)
				continue
			}

			p.syncMutex.Lock()
			if p.syncInProgress {
				p.syncMutex.Unlock()
				logx.Infof("上次同步未完成，跳过本次 [prefix=%s]", p.prefix)
				interval = min(interval*2, maxInterval)
				ticker.Reset(interval)
				continue
			}
			p.syncInProgress = true
			p.syncMutex.Unlock()

			start := time.Now()

			if err := p.processSyncQueue(); err != nil {
				logx.Errorf("同步失败 [prefix=%s]: %v", p.prefix, err)
			}

			duration := time.Since(start)

			remainingCount, _ := p.GetPendingSyncCount()

			p.syncMutex.Lock()
			p.syncInProgress = false
			p.syncMutex.Unlock()

			// 🔧 动态调整间隔
			if remainingCount > 0 {
				interval = minInterval
			} else if duration < interval/2 {
				interval = max(interval/2, minInterval)
			} else if duration > interval {
				interval = min(duration*2, maxInterval)
			}

			ticker.Reset(interval)
			logx.Infof("同步完成 [prefix=%s, 处理: %d, 剩余: %d, 耗时: %v, 下次间隔: %v]",
				p.prefix, pendingCount, remainingCount, duration, interval)

		case <-p.closeCh:
			// ✅ 优化：添加超时保护，避免无限等待
			logx.Infof("收到关闭信号 [prefix=%s]", p.prefix)

			p.syncMutex.Lock()
			if p.syncInProgress {
				p.syncMutex.Unlock()

				// 等待同步完成，最多等待 10 秒
				logx.Infof("等待当前同步操作完成 [prefix=%s]", p.prefix)
				timeout := time.After(10 * time.Second)
				ticker := time.NewTicker(100 * time.Millisecond)
				defer ticker.Stop()

				for {
					select {
					case <-timeout:
						logx.Errorf("等待同步完成超时（10秒），强制退出 [prefix=%s]", p.prefix)
						return
					case <-ticker.C:
						p.syncMutex.Lock()
						if !p.syncInProgress {
							p.syncMutex.Unlock()
							logx.Infof("同步操作已完成 [prefix=%s]", p.prefix)
							logx.Infof("syncToOtherDB 退出 [prefix=%s]", p.prefix)
							return
						}
						p.syncMutex.Unlock()
					}
				}
			} else {
				p.syncMutex.Unlock()
			}

			logx.Infof("syncToOtherDB 退出 [prefix=%s]", p.prefix)
			return
		}
	}
}

// 🔧 修复 processSyncQueue: 返回同步成功的数量
func (p *PrefixedBadgerDB[T]) processSyncQueue() error {
	unsyncedItems, err := p.getUnsyncedBatch(p.manager.config.SyncBatchSize)
	if err != nil {
		return fmt.Errorf("获取未同步数据失败: %w", err)
	}

	if len(unsyncedItems) == 0 {
		return nil
	}

	logx.Infof("开始同步 [prefix=%s, 数量: %d]", p.prefix, len(unsyncedItems))

	_, err = p.syncBatch(unsyncedItems)
	if err != nil {
		logx.Errorf("批量同步失败 [prefix=%s]: %v", p.prefix, err)
		return err
	}
	successCount := 0
	for _, item := range unsyncedItems {
		if item.IsSynced {
			successCount++
		}
	}
	logx.Infof("同步成功 [prefix=%s, 成功: %d/%d]", p.prefix, successCount, len(unsyncedItems))
	return nil
}

// 🔧 修复 GetPendingSyncCount: 添加超时保护
func (p *PrefixedBadgerDB[T]) GetPendingSyncCount() (int, error) {
	count := 0

	// 🆕 添加超时保护
	done := make(chan error, 1)

	go func() {
		err := p.manager.db.View(func(txn *badger.Txn) error {
			opts := badger.DefaultIteratorOptions
			opts.PrefetchValues = true
			opts.PrefetchSize = 100 // 🔧 限制预取大小
			it := txn.NewIterator(opts)
			defer it.Close()

			for it.Seek([]byte(p.prefix)); it.ValidForPrefix([]byte(p.prefix)); it.Next() {
				item := it.Item()

				err := item.Value(func(val []byte) error {
					var wrapper SyncQueueItem[T]
					if err := json.Unmarshal(val, &wrapper); err != nil {
						return err
					}

					if !wrapper.IsSynced {
						count++
					}
					return nil
				})

				if err != nil {
					continue
				}
			}
			return nil
		})
		done <- err
	}()

	// 🆕 等待完成或超时
	select {
	case err := <-done:
		return count, err
	case <-time.After(5 * time.Second):
		return 0, fmt.Errorf("获取待同步数量超时 [prefix=%s]", p.prefix)
	}
}

// getUnsyncedBatch 获取未同步数据（限定前缀）
func (p *PrefixedBadgerDB[T]) getUnsyncedBatch(limit int) ([]*SyncQueueItem[T], error) {
	var items []*SyncQueueItem[T]

	err := p.manager.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		count := 0
		for it.Seek([]byte(p.prefix)); it.ValidForPrefix([]byte(p.prefix)); it.Next() {
			if count >= limit {
				break
			}

			item := it.Item()

			err := item.Value(func(val []byte) error {
				var wrapper SyncQueueItem[T]
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return err
				}

				if !wrapper.IsSynced {
					if wrapper.Item != nil {
						if hook, ok := any(wrapper.Item).(types.IModelNewHook); ok {
							hook.NewModel()
						}
					}
					items = append(items, &wrapper)
					count++
				}
				return nil
			})

			if err != nil {
				continue
			}
		}
		return nil
	})

	return items, err
}

// 🔧 完整的批量同步实现（包含错误处理）
func (p *PrefixedBadgerDB[T]) syncBatch(items []*SyncQueueItem[T]) ([]string, error) {
	if len(items) == 0 {
		return nil, nil
	}

	p.syncLock.RLock()
	if !p.syncDB {
		p.syncLock.RUnlock()
		return nil, fmt.Errorf("未开启 syncDB")
	}
	p.syncLock.RUnlock()

	// 按操作类型分组
	var (
		insertItems []*SyncQueueItem[T]
		updateItems []*SyncQueueItem[T]
		deleteItems []*SyncQueueItem[T]
	)

	for _, wrapper := range items {
		setHashCode(wrapper.Item)

		switch wrapper.Op {
		case OpInsert:
			insertItems = append(insertItems, wrapper)
		case OpUpdate:
			updateItems = append(updateItems, wrapper)
		case OpDelete:
			deleteItems = append(deleteItems, wrapper)
		}
	}

	successKeys := make([]string, 0, len(items))

	// 批量插入（带错误处理）
	if len(insertItems) > 0 {
		keys := p.batchInsertWithErrorHandling(insertItems)
		successKeys = append(successKeys, keys...)
		p.onSyncAfter(insertItems)
	}

	// 批量更新（带错误处理）
	if len(updateItems) > 0 {
		keys := p.batchUpdateWithErrorHandling(updateItems)
		successKeys = append(successKeys, keys...)
		p.onSyncAfter(updateItems)
	}

	// 批量删除（带错误处理）
	if len(deleteItems) > 0 {
		keys := p.batchDeleteWithErrorHandling(deleteItems)
		successKeys = append(successKeys, keys...)
		p.onSyncAfter(deleteItems)
	}

	// 批量更新同步状态
	if len(successKeys) > 0 {
		p.batchUpdateSyncedStatus(successKeys)
	}
	return successKeys, nil
}
func (p *PrefixedBadgerDB[T]) onSyncAfter(items []*SyncQueueItem[T]) {
	if len(items) == 0 {
		return
	}
	if iosa, ok := any(items[0].Item).(IOnSyncAfter[T]); ok {
		//如果异常不影响主流程，可以忽略错误
		// defer func() {
		// 	if err := recover(); err != nil {
		// 		logx.Errorf("执行 OnSyncAfter 发生恐慌: %v", err)
		// 	}
		// }()
		err := iosa.OnSyncAfter(items)
		if err != nil {
			logx.Errorf("执行 OnSyncAfter 失败: %v", err)
		}
	}
}
func setHashCode(item any) {
	if item == nil {
		return
	}
	if rowCode, ok := item.(types.IRowCode); ok {
		hash := rowCode.GetHash()
		if hash == "" {
			logx.Errorf("IRowCode GetHash 返回空字符串，可能导致 key 生成失败")
		}
		rowCode.SetHashcode(hash)
	}
}

// 🆕 批量插入（使用事务）
func (p *PrefixedBadgerDB[T]) batchInsertWithErrorHandling(items []*SyncQueueItem[T]) []string {
	if len(items) == 0 {
		return nil
	}

	syncAction := p.getDataAction(items[0].Item)
	if syncAction == nil {
		logx.Errorf("未找到同步操作对象")
		return nil
	}

	successKeys := make([]string, 0, len(items))
	physicalDeleteKeys := make([]string, 0, len(items))

	// 🔧 开启事务（批量操作）
	if err := syncAction.Transaction(); err != nil {
		logx.Errorf("开启事务失败: %v，降级为逐条插入", err)
		return p.insertItemsOneByOne(items)
	}

	// 在事务中逐条插入
	hasError := false
	for _, wrapper := range items {
		if wrapper.Item == nil {
			continue
		}

		err := syncAction.Insert(wrapper.Item)
		if err != nil {
			// 🔧 处理主键冲突 - 尝试更新
			if strings.Contains(err.Error(), "duplicate key") ||
				strings.Contains(err.Error(), "UNIQUE constraint failed") {
				logx.Infof("插入冲突，尝试更新 [%s]", wrapper.Key)

				err = syncAction.Update(wrapper.Item)
				if err == nil {
					// 🔧 修复：更新成功后也要检查是否物理删除
					if syncAfterDelete, ok := any(wrapper.Item).(ISyncAfterDelete[T]); ok {
						if needDelete := syncAfterDelete.IsSyncAfterDelete(); needDelete {
							physicalDeleteKeys = append(physicalDeleteKeys, wrapper.Key)
							continue // 不加入 successKeys
						}
					}
					successKeys = append(successKeys, wrapper.Key)
					continue
				}

				logx.Errorf("更新失败 [%s]: %v", wrapper.Key, err)
				hasError = true
				continue
			}

			// 其他错误
			logx.Errorf("插入失败 [%s]: %v", wrapper.Key, err)
			hasError = true
			continue
		}
		wrapper.IsSynced = true
		// 🔧 插入成功，检查是否需要物理删除
		if syncAfterDelete, ok := any(wrapper.Item).(ISyncAfterDelete[T]); ok {
			if needDelete := syncAfterDelete.IsSyncAfterDelete(); needDelete {
				logx.Infof("ISyncAfterDelete 返回 true，将物理删除 [%s]", wrapper.Key)
				physicalDeleteKeys = append(physicalDeleteKeys, wrapper.Key)
				continue // 🔧 不加入 successKeys
			}
		}
		// 插入成功且不需要物理删除
		successKeys = append(successKeys, wrapper.Key)

	}

	// 🔧 提交事务
	if err := syncAction.Commit(); err != nil {
		logx.Errorf("提交事务失败: %v，回滚并降级为逐条处理", err)

		if rollbackErr := syncAction.Rollback(); rollbackErr != nil {
			logx.Errorf("回滚失败: %v", rollbackErr)
		}
		successKeys = p.insertItemsOneByOne(items)
	}

	// 🆕 批量物理删除本地缓存
	if len(physicalDeleteKeys) > 0 {
		if err := p.batchDelete(physicalDeleteKeys); err != nil {
			logx.Errorf("批量物理删除本地缓存失败: %v", err)
		}
	}

	if hasError {
		logx.Errorf("批量插入部分失败，成功: %d, 物理删除: %d, 总数: %d",
			len(successKeys), len(physicalDeleteKeys), len(items))
	}

	// 🔧 减少待同步计数
	count := len(successKeys) + len(physicalDeleteKeys)
	p.incrementPendingCount(-count)

	return successKeys // 🔧 只返回需要更新同步状态的 keys
}

// 🆕 带超时的批量更新
func (p *PrefixedBadgerDB[T]) batchUpdateWithErrorHandling(items []*SyncQueueItem[T]) []string {
	if len(items) == 0 {
		return nil
	}

	syncAction := p.getDataAction(items[0].Item)
	if syncAction == nil {
		logx.Errorf("未找到同步操作对象")
		return nil
	}

	successKeys := make([]string, 0, len(items))
	physicalDeleteKeys := make([]string, 0, len(items))

	// 🆕 添加超时保护
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 🔧 开启事务
	if err := syncAction.Transaction(); err != nil {
		logx.Errorf("开启事务失败: %v，降级为逐条更新", err)
		return p.updateItemsOneByOne(items)
	}

	// 🆕 确保事务一定被提交或回滚
	committed := false
	defer func() {
		if !committed {
			logx.Errorf("检测到未提交的事务，自动回滚")
			syncAction.Rollback()
		}
	}()

	// 🆕 使用通道接收结果
	done := make(chan struct{})
	hasError := false

	go func() {
		// 在事务中逐条更新
		for _, wrapper := range items {
			select {
			case <-ctx.Done():
				logx.Errorf("更新超时，停止批量更新")
				hasError = true
				return
			default:
			}

			if wrapper.Item == nil {
				continue
			}

			err := syncAction.Update(wrapper.Item)

			if err != nil {
				// 🔧 检查事务错误
				if strings.Contains(err.Error(), "transaction has already been committed") ||
					strings.Contains(err.Error(), "transaction has already been rolled back") {
					logx.Errorf("事务已失效，停止批量更新 [%s]", wrapper.Key)
					hasError = true
					return
				}

				// 🔧 检查连接错误
				if strings.Contains(err.Error(), "Rows are closed") ||
					strings.Contains(err.Error(), "context canceled") {
					logx.Errorf("连接已关闭，停止批量更新 [%s]", wrapper.Key)
					hasError = true
					return
				}

				// 🔧 处理记录不存在
				if strings.Contains(err.Error(), "record not found") ||
					strings.Contains(err.Error(), "no rows") {
					logx.Infof("记录不存在，尝试插入 [%s]", wrapper.Key)

					err = syncAction.Insert(wrapper.Item)
					if err == nil {
						if syncAfterDelete, ok := any(wrapper.Item).(ISyncAfterDelete[T]); ok {
							if needDelete := syncAfterDelete.IsSyncAfterDelete(); needDelete {
								physicalDeleteKeys = append(physicalDeleteKeys, wrapper.Key)
								continue
							}
						}
						successKeys = append(successKeys, wrapper.Key)
						continue
					}

					// 插入也失败
					if strings.Contains(err.Error(), "duplicate key") ||
						strings.Contains(err.Error(), "UNIQUE constraint failed") {
						logx.Errorf("插入冲突，重试更新 [%s]", wrapper.Key)
						err = syncAction.Update(wrapper.Item)
						if err == nil {
							if syncAfterDelete, ok := any(wrapper.Item).(ISyncAfterDelete[T]); ok {
								if needDelete := syncAfterDelete.IsSyncAfterDelete(); needDelete {
									physicalDeleteKeys = append(physicalDeleteKeys, wrapper.Key)
									continue
								}
							}
							successKeys = append(successKeys, wrapper.Key)
							continue
						}
					}

					logx.Errorf("插入失败 [%s]: %v", wrapper.Key, err)
					hasError = true
					continue
				}

				logx.Errorf("更新失败 [%s]: %v", wrapper.Key, err)
				hasError = true
				continue
			}

			wrapper.IsSynced = true

			// 更新成功，检查是否需要物理删除
			if syncAfterDelete, ok := any(wrapper.Item).(ISyncAfterDelete[T]); ok {
				if needDelete := syncAfterDelete.IsSyncAfterDelete(); needDelete {
					logx.Infof("ISyncAfterDelete 返回 true，将物理删除 [%s]", wrapper.Key)
					physicalDeleteKeys = append(physicalDeleteKeys, wrapper.Key)
					continue
				}
			}

			successKeys = append(successKeys, wrapper.Key)
		}
		close(done)
	}()

	// 🆕 等待完成或超时
	select {
	case <-done:
		// 正常完成
	case <-ctx.Done():
		logx.Errorf("批量更新超时 [数量: %d]", len(items))
		hasError = true
	}

	// 🔧 提交或回滚事务
	if hasError {
		logx.Errorf("批量更新遇到错误，回滚事务")
		if rollbackErr := syncAction.Rollback(); rollbackErr != nil {
			logx.Errorf("回滚失败: %v", rollbackErr)
		}
		committed = true
		return p.updateItemsOneByOne(items)
	}

	if err := syncAction.Commit(); err != nil {
		logx.Errorf("提交事务失败: %v", err)
		committed = true

		if rollbackErr := syncAction.Rollback(); rollbackErr != nil {
			logx.Errorf("回滚失败: %v", rollbackErr)
		}

		return p.updateItemsOneByOne(items)
	}

	committed = true

	// 🆕 批量物理删除
	if len(physicalDeleteKeys) > 0 {
		if err := p.batchDelete(physicalDeleteKeys); err != nil {
			logx.Errorf("批量物理删除失败: %v", err)
		}
	}

	if hasError {
		logx.Errorf("批量更新部分失败，成功: %d, 物理删除: %d, 总数: %d",
			len(successKeys), len(physicalDeleteKeys), len(items))
	}

	count := len(successKeys) + len(physicalDeleteKeys)
	p.incrementPendingCount(-count)

	return successKeys
}

// 🆕 批量删除（使用事务）
func (p *PrefixedBadgerDB[T]) batchDeleteWithErrorHandling(items []*SyncQueueItem[T]) []string {
	if len(items) == 0 {
		return nil
	}

	syncAction := p.getDataAction(items[0].Item)
	if syncAction == nil {
		logx.Errorf("未找到同步操作对象")
		return nil
	}

	successKeys := make([]string, 0, len(items))

	// 🔧 开启事务（批量操作）
	if err := syncAction.Transaction(); err != nil {
		logx.Errorf("开启事务失败: %v，降级为逐条删除", err)
		newSyncAction := p.getDataAction(items[0].Item)
		return p.deleteItemsOneByOne(items, newSyncAction)
	}

	// 在事务中逐条删除
	hasError := false
	for _, wrapper := range items {
		if wrapper.Item == nil {
			continue
		}

		err := syncAction.Delete(wrapper.Item)
		if err == nil {
			// 删除成功
			successKeys = append(successKeys, wrapper.Key)
			wrapper.IsSynced = true
		}
		if err != nil {
			// 🔧 处理记录不存在 - 视为成功
			if strings.Contains(err.Error(), "record not found") ||
				strings.Contains(err.Error(), "no rows") {
				logx.Infof("删除目标不存在，跳过 [%s]", wrapper.Key)
				successKeys = append(successKeys, wrapper.Key)
				continue
			}
			logx.Errorf("删除失败 [%s]: %v", wrapper.Key, err)
			hasError = true
			continue
		}

	}

	// 🔧 提交事务
	if err := syncAction.Commit(); err != nil {
		logx.Errorf("提交事务失败: %v，回滚并降级为逐条处理", err)

		if rollbackErr := syncAction.Rollback(); rollbackErr != nil {
			logx.Errorf("回滚失败: %v", rollbackErr)
		}

		// 🆕 重新获取新的 syncAction
		newSyncAction := p.getDataAction(items[0].Item)
		if newSyncAction == nil {
			logx.Errorf("重新获取 syncAction 失败")
			return nil
		}

		successKeys = p.deleteItemsOneByOne(items, newSyncAction)
	}

	// 物理删除本地缓存
	for _, key := range successKeys {
		if err := p.delete(key, false); err != nil {
			logx.Errorf("物理删除本地缓存失败 [%s]: %v", key, err)
		}
	}

	if hasError {
		logx.Errorf("批量删除部分失败，成功: %d/%d", len(successKeys), len(items))
	}
	count := len(successKeys)
	p.incrementPendingCount(-count)
	return successKeys
}

// 🆕 逐条插入（无事务）
func (p *PrefixedBadgerDB[T]) insertItemsOneByOne(items []*SyncQueueItem[T]) []string {
	successKeys := make([]string, 0, len(items))

	for _, wrapper := range items {
		if wrapper.Item == nil {
			continue
		}
		wrapper.IsSynced = false
		syncAction := p.getDataAction(wrapper.Item)
		err := syncAction.Insert(wrapper.Item)
		if err != nil {
			// 🔧 处理主键冲突 - 尝试更新
			if strings.Contains(err.Error(), "duplicate key") ||
				strings.Contains(err.Error(), "UNIQUE constraint failed") {
				logx.Infof("插入冲突，尝试更新 [%s]", wrapper.Key)

				err = syncAction.Update(wrapper.Item)
				if err == nil {
					successKeys = append(successKeys, wrapper.Key)
					continue
				}

				logx.Errorf("更新失败 [%s]: %v", wrapper.Key, err)
				continue
			}

			logx.Errorf("插入失败 [%s]: %v", wrapper.Key, err)
			continue
		}
		wrapper.IsSynced = true
		if syncAfterDelete, ok := any(wrapper.Item).(ISyncAfterDelete[T]); ok {
			if needDelete := syncAfterDelete.IsSyncAfterDelete(); needDelete {
				err := p.delete(wrapper.Key, false)
				if err != nil {
					logx.Errorf("物理删除本地缓存失败 [%s]: %v", wrapper.Key, err)
				}
				continue
			}
		}
		successKeys = append(successKeys, wrapper.Key)

	}

	return successKeys
}

// 🆕 逐条更新（无事务）
func (p *PrefixedBadgerDB[T]) updateItemsOneByOne(items []*SyncQueueItem[T]) []string {
	successKeys := make([]string, 0, len(items))

	for _, wrapper := range items {
		if wrapper.Item == nil {
			continue
		}
		wrapper.IsSynced = false
		syncAction := p.getDataAction(wrapper.Item)
		err := syncAction.Update(wrapper.Item)

		if err != nil {
			// 🔧 处理记录不存在 - 尝试插入
			if strings.Contains(err.Error(), "record not found") ||
				strings.Contains(err.Error(), "no rows") {
				logx.Infof("记录不存在，尝试插入 [%s]", wrapper.Key)

				err = syncAction.Insert(wrapper.Item)
				if err == nil {
					successKeys = append(successKeys, wrapper.Key)
					continue
				}

				// 插入也失败（可能是主键冲突，再尝试更新）
				if strings.Contains(err.Error(), "duplicate key") ||
					strings.Contains(err.Error(), "UNIQUE constraint failed") {
					logx.Errorf("插入冲突，重试更新 [%s]", wrapper.Key)
					err = syncAction.Update(wrapper.Item)
					if err == nil {
						successKeys = append(successKeys, wrapper.Key)
						continue
					}
				}

				logx.Errorf("插入失败 [%s]: %v", wrapper.Key, err)
				continue
			}

			logx.Errorf("更新失败 [%s]: %v", wrapper.Key, err)
			continue
		}
		wrapper.IsSynced = true
		if syncAfterDelete, ok := any(wrapper.Item).(ISyncAfterDelete[T]); ok {
			if needDelete := syncAfterDelete.IsSyncAfterDelete(); needDelete {
				err := p.delete(wrapper.Key, false)
				if err != nil {
					logx.Errorf("物理删除本地缓存失败 [%s]: %v", wrapper.Key, err)
				}
				continue
			}
		}
		successKeys = append(successKeys, wrapper.Key)

	}

	return successKeys
}

// 🆕 逐条删除（无事务）
func (p *PrefixedBadgerDB[T]) deleteItemsOneByOne(items []*SyncQueueItem[T], syncAction types.IDataAction) []string {
	successKeys := make([]string, 0, len(items))

	for _, wrapper := range items {
		if wrapper.Item == nil {
			continue
		}
		wrapper.IsSynced = false
		err := syncAction.Delete(wrapper.Item)

		if err != nil {
			// 🔧 处理记录不存在 - 视为成功
			if strings.Contains(err.Error(), "record not found") ||
				strings.Contains(err.Error(), "no rows") {
				logx.Infof("删除目标不存在，跳过 [%s]", wrapper.Key)
				successKeys = append(successKeys, wrapper.Key)

				// 物理删除本地缓存
				if err1 := p.delete(wrapper.Key, false); err1 != nil {
					logx.Errorf("物理删除本地缓存失败 [%s]: %v", wrapper.Key, err1)
				}
				continue
			}

			// 🔧 处理 WHERE 条件缺失 - 这是编程错误
			if strings.Contains(err.Error(), "WHERE conditions required") {
				logx.Errorf("删除条件缺失 [%s]，需要检查 Delete 实现: %v", wrapper.Key, err)
				// 不加入成功列表，等待重试
				continue
			}

			logx.Errorf("删除失败 [%s]: %v", wrapper.Key, err)
			continue
		}

		// 删除成功
		successKeys = append(successKeys, wrapper.Key)
		wrapper.IsSynced = true
		// 物理删除本地缓存
		if err1 := p.delete(wrapper.Key, false); err1 != nil {
			logx.Errorf("物理删除本地缓存失败 [%s]: %v", wrapper.Key, err1)
		}
	}

	return successKeys
}

// 🆕 批量更新同步状态（一次 BadgerDB 事务）
func (p *PrefixedBadgerDB[T]) batchUpdateSyncedStatus(keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	now := time.Now()

	return p.manager.db.Update(func(txn *badger.Txn) error {
		for _, key := range keys {
			item, err := txn.Get([]byte(key))
			if err != nil {
				if err == badger.ErrKeyNotFound {
					continue
				}
				logx.Errorf("获取key失败 [%s]: %v", key, err)
				continue
			}

			var wrapper SyncQueueItem[T]
			err = item.Value(func(val []byte) error {
				return json.Unmarshal(val, &wrapper)
			})
			if err != nil {
				logx.Errorf("反序列化失败 [%s]: %v", key, err)
				continue
			}

			// 更新同步状态
			wrapper.IsSynced = true
			wrapper.SyncedAt = now

			data, err := json.Marshal(&wrapper)
			if err != nil {
				logx.Errorf("序列化失败 [%s]: %v", key, err)
				continue
			}

			if err := txn.Set([]byte(key), data); err != nil {
				logx.Errorf("更新同步状态失败 [%s]: %v", key, err)
			}
		}
		return nil
	})
}

func (p *PrefixedBadgerDB[T]) Count() (int, error) {
	count := 0

	err := p.manager.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek([]byte(p.prefix)); it.ValidForPrefix([]byte(p.prefix)); it.Next() {
			count++
		}
		return nil
	})

	return count, err
}
func (p *PrefixedBadgerDB[T]) CountByPrefix(subPrefix string) (int, error) {
	count := 0
	fullPrefix := p.prefix + subPrefix

	err := p.manager.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true // 🔧 需要读取值来判断是否删除
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek([]byte(fullPrefix)); it.ValidForPrefix([]byte(fullPrefix)); it.Next() {
			item := it.Item()

			// 🆕 解析数据，检查是否已删除
			err := item.Value(func(val []byte) error {
				var wrapper SyncQueueItem[T]
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return err
				}

				// 只统计未删除的数据
				if !wrapper.IsDeleted {
					count++
				}
				return nil
			})

			if err != nil {
				logx.Errorf("解析数据失败: %v", err)
				continue
			}
		}
		return nil
	})

	return count, err
}

// Close 关闭实例
func (p *PrefixedBadgerDB[T]) Close() error {
	return p.CloseWithTimeout(30*time.Second, 10*time.Second)
}

// CloseWithTimeout 带超时的关闭实例
func (p *PrefixedBadgerDB[T]) CloseWithTimeout(waitTimeout, syncTimeout time.Duration) error {
	// ✅ 使用 sync.Once 确保只关闭一次
	p.closeOnce.Do(func() {
		// 标记为已关闭
		p.closeMu.Lock()
		p.closed = true
		p.closeMu.Unlock()

		// 关闭 channel，通知 goroutine 退出
		close(p.closeCh)

		// ✅ 等待 goroutine 退出（带超时）
		done := make(chan struct{})
		go func() {
			p.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			logx.Infof("后台同步 goroutine 已退出 [prefix=%s]", p.prefix)
		case <-time.After(waitTimeout):
			logx.Errorf("等待后台 goroutine 退出超时（%v），强制关闭 [prefix=%s]", waitTimeout, p.prefix)

			// ✅ 检查是否还在同步中
			p.syncMutex.Lock()
			if p.syncInProgress {
				logx.Errorf("检测到正在进行的同步操作（等待最多 %v）[prefix=%s]", syncTimeout, p.prefix)

				// 等待同步完成或超时
				syncDone := make(chan struct{})
				go func() {
					for p.syncInProgress {
						time.Sleep(100 * time.Millisecond)
					}
					close(syncDone)
				}()

				select {
				case <-syncDone:
					logx.Infof("同步操作已完成 [prefix=%s]", p.prefix)
				case <-time.After(syncTimeout):
					logx.Errorf("等待同步完成超时（%v），强制退出 [prefix=%s]", syncTimeout, p.prefix)
				}
			}
			p.syncMutex.Unlock()
		}

		// 移除管理器引用
		p.manager.RemoveRef(p.prefix)

		logx.Infof("共享BadgerDB实例已关闭 [prefix=%s]", p.prefix)
	})

	return nil
}

// IsClosed 检查实例是否已关闭
func (p *PrefixedBadgerDB[T]) IsClosed() bool {
	p.closeMu.RLock()
	defer p.closeMu.RUnlock()
	return p.closed
}

// 🆕 CheckAndEnforceLimit 检查并执行数量限制
func (p *PrefixedBadgerDB[T]) CheckAndEnforceLimit(model *T) error {
	// 检查模型是否实现了 IAutoLimit 接口

	limitConfig, ok := any(model).(IAutoLimit[T])
	if !ok {
		return nil // 未实现接口,不执行限制
	}

	filterPrefix, maxCount, sortField, descending := limitConfig.GetLimitConfig()
	if maxCount <= 0 {
		return nil // 无限制
	}

	// 统计当前数量
	currentCount, err := p.CountByPrefix(filterPrefix)
	if err != nil {
		return fmt.Errorf("统计数量失败: %w", err)
	}

	if currentCount <= maxCount {
		return nil // 未超过限制
	}

	// 需要删除的数量
	deleteCount := currentCount - maxCount
	logx.Infof("数据超限 [prefix=%s, current=%d, max=%d, delete=%d]",
		p.prefix+filterPrefix, currentCount, maxCount, deleteCount)

	// 获取需要删除的旧数据
	keysToDelete, err := p.getOldestKeys(filterPrefix, deleteCount, sortField, descending)
	if err != nil {
		return fmt.Errorf("获取旧数据失败: %w", err)
	}

	if len(keysToDelete) == 0 {
		return nil
	}

	// 批量删除
	if err := p.batchDelete(keysToDelete); err != nil {
		return fmt.Errorf("批量删除失败: %w", err)
	}

	logx.Infof("自动清理完成 [prefix=%s, deleted=%d]", p.prefix+filterPrefix, len(keysToDelete))
	return nil
}

// 🆕 获取最旧的数据key列表
func (p *PrefixedBadgerDB[T]) getOldestKeys(filterPrefix string, count int, sortField string, descending bool) ([]string, error) {
	type itemWithKey struct {
		key       string
		sortValue interface{}
		timestamp time.Time
	}

	fullPrefix := p.prefix + filterPrefix
	items := make([]itemWithKey, 0)

	err := p.manager.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek([]byte(fullPrefix)); it.ValidForPrefix([]byte(fullPrefix)); it.Next() {
			item := it.Item()
			key := string(item.Key())

			err := item.Value(func(val []byte) error {
				var wrapper SyncQueueItem[T]
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return err
				}

				// 跳过已删除的数据
				if wrapper.IsDeleted {
					return nil
				}

				// 提取排序字段值
				var sortValue interface{}
				var timestamp time.Time

				if wrapper.Item != nil {
					// 使用反射获取排序字段
					v := reflect.ValueOf(wrapper.Item)
					if v.Kind() == reflect.Ptr {
						v = v.Elem()
					}

					if v.Kind() == reflect.Struct {
						field := v.FieldByName(sortField)
						if field.IsValid() {
							sortValue = field.Interface()
							// 如果是时间类型
							if t, ok := sortValue.(time.Time); ok {
								timestamp = t
							}
						}
					}
				}

				// 如果没有找到排序字段,使用创建时间
				if timestamp.IsZero() {
					timestamp = wrapper.CreatedAt
				}

				items = append(items, itemWithKey{
					key:       key,
					sortValue: sortValue,
					timestamp: timestamp,
				})
				return nil
			})

			if err != nil {
				logx.Errorf("解析数据失败: %v", err)
				continue
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	// 排序(根据时间戳)
	if descending {
		// 降序: 保留最新的,删除最旧的
		for i := 0; i < len(items); i++ {
			for j := i + 1; j < len(items); j++ {
				if items[i].timestamp.Before(items[j].timestamp) {
					items[i], items[j] = items[j], items[i]
				}
			}
		}
	} else {
		// 升序: 保留最旧的,删除最新的
		for i := 0; i < len(items); i++ {
			for j := i + 1; j < len(items); j++ {
				if items[i].timestamp.After(items[j].timestamp) {
					items[i], items[j] = items[j], items[i]
				}
			}
		}
	}

	// 取需要删除的key
	deleteCount := count
	if deleteCount > len(items) {
		deleteCount = len(items)
	}

	keys := make([]string, deleteCount)
	for i := 0; i < deleteCount; i++ {
		keys[i] = items[len(items)-deleteCount+i].key
	}

	return keys, nil
}

// 🆕 在 Set 方法后自动检查限制
func (p *PrefixedBadgerDB[T]) SetWithAutoLimit(item *T, ttl time.Duration, fn ...func(wrapper *SyncQueueItem[T])) error {
	err := p.Set(item, ttl, fn...)
	if err != nil {
		return err
	}

	// 异步检查并执行限制
	go func() {
		if err := p.CheckAndEnforceLimit(item); err != nil {
			logx.Errorf("自动限制检查失败: %v", err)
		}
	}()

	return nil
}
