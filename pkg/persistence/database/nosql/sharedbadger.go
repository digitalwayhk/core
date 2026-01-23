package nosql

import (
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
	var results []*T
	prefix = p.prefix + prefix
	err := p.manager.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 100
		opts.PrefetchValues = true
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

// GetPendingSyncCount 获取待同步数量
func (p *PrefixedBadgerDB[T]) GetPendingSyncCount() (int, error) {
	count := 0

	err := p.manager.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
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

	return count, err
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

// syncToOtherDB 同步到其他数据库（复用原有逻辑）
func (p *PrefixedBadgerDB[T]) syncToOtherDB() {
	defer p.wg.Done()

	config := p.manager.config
	interval := config.SyncInterval
	minInterval := config.SyncMinInterval
	maxInterval := config.SyncMaxInterval

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

			p.syncMutex.Lock()
			p.syncInProgress = false
			p.syncMutex.Unlock()

			if duration < interval/2 {
				interval = max(interval/2, minInterval)
			} else if duration > interval {
				interval = min(duration*2, maxInterval)
			}

			ticker.Reset(interval)
			logx.Infof("同步完成 [prefix=%s, 处理: %d, 耗时: %v]", p.prefix, pendingCount, duration)

		case <-p.closeCh:
			logx.Infof("syncToOtherDB 退出 [prefix=%s]", p.prefix)
			return
		}
	}
}

// processSyncQueue 处理同步队列（复用原有逻辑，限定前缀）
func (p *PrefixedBadgerDB[T]) processSyncQueue() error {
	unsyncedItems, err := p.getUnsyncedBatch(p.manager.config.SyncBatchSize)
	if err != nil {
		return fmt.Errorf("获取未同步数据失败: %w", err)
	}

	if len(unsyncedItems) == 0 {
		return nil
	}

	_, err = p.syncBatch(unsyncedItems)
	return err
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
	}

	// 批量更新（带错误处理）
	if len(updateItems) > 0 {
		keys := p.batchUpdateWithErrorHandling(updateItems)
		successKeys = append(successKeys, keys...)
	}

	// 批量删除（带错误处理）
	if len(deleteItems) > 0 {
		keys := p.batchDeleteWithErrorHandling(deleteItems)
		successKeys = append(successKeys, keys...)
	}

	// 批量更新同步状态
	if len(successKeys) > 0 {
		p.batchUpdateSyncedStatus(successKeys)
		p.incrementPendingCount(-len(successKeys))
	}

	return successKeys, nil
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
	physicalDeleteKeys := make([]string, 0, len(items)) // 🆕 需要物理删除的keys

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

		// 🆕 检查是否实现 ISyncAfterDelete 接口
		shouldPhysicalDelete := false
		if syncAfterDelete, ok := any(wrapper.Item).(ISyncAfterDelete[T]); ok {
			if needDelete := syncAfterDelete.IsSyncAfterDelete(); needDelete {
				logx.Infof("ISyncAfterDelete 返回 true，将物理删除 [%s]", wrapper.Key)
				shouldPhysicalDelete = true
			}
		}

		if shouldPhysicalDelete {
			physicalDeleteKeys = append(physicalDeleteKeys, wrapper.Key)
		}
		if !shouldPhysicalDelete {
			// 插入成功
			successKeys = append(successKeys, wrapper.Key)
		}
	}

	// 🔧 提交事务
	if err := syncAction.Commit(); err != nil {
		logx.Errorf("提交事务失败: %v，回滚并降级为逐条处理", err)

		// 安全回滚
		if rollbackErr := syncAction.Rollback(); rollbackErr != nil {
			logx.Errorf("回滚失败: %v", rollbackErr)
		}
		return p.insertItemsOneByOne(items)
	}
	// 🆕 批量物理删除本地缓存
	if len(physicalDeleteKeys) > 0 {
		if err := p.batchDelete(physicalDeleteKeys); err != nil {
			logx.Errorf("批量物理删除本地缓存失败: %v", err)
		}
	}
	if hasError {
		logx.Errorf("批量插入部分失败，成功: %d/%d", len(successKeys), len(items))
	}

	return successKeys
}

// 🆕 批量更新（使用事务）
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
	physicalDeleteKeys := make([]string, 0, len(items)) // 🆕 需要物理删除的keys

	// 🔧 开启事务（批量操作）
	if err := syncAction.Transaction(); err != nil {
		logx.Errorf("开启事务失败: %v，降级为逐条更新", err)
		return p.updateItemsOneByOne(items)
	}

	// 在事务中逐条更新
	hasError := false
	for _, wrapper := range items {
		if wrapper.Item == nil {
			continue
		}

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
				hasError = true
				continue
			}

			logx.Errorf("更新失败 [%s]: %v", wrapper.Key, err)
			hasError = true
			continue
		}

		// 🆕 检查是否实现 ISyncAfterDelete 接口
		shouldPhysicalDelete := false
		if syncAfterDelete, ok := any(wrapper.Item).(ISyncAfterDelete[T]); ok {
			if needDelete := syncAfterDelete.IsSyncAfterDelete(); needDelete {
				logx.Infof("ISyncAfterDelete 返回 true，将物理删除 [%s]", wrapper.Key)
				shouldPhysicalDelete = true
			}
		}
		if shouldPhysicalDelete {
			physicalDeleteKeys = append(physicalDeleteKeys, wrapper.Key)
		}
		if !shouldPhysicalDelete {
			// 更新成功
			successKeys = append(successKeys, wrapper.Key)
		}
	}

	// 🔧 提交事务
	if err := syncAction.Commit(); err != nil {
		logx.Errorf("提交事务失败: %v，回滚并降级为逐条处理", err)

		if rollbackErr := syncAction.Rollback(); rollbackErr != nil {
			logx.Errorf("回滚失败: %v", rollbackErr)
		}
		return p.updateItemsOneByOne(items)
	}
	// 🆕 批量物理删除本地缓存
	if len(physicalDeleteKeys) > 0 {
		if err := p.batchDelete(physicalDeleteKeys); err != nil {
			logx.Errorf("批量物理删除本地缓存失败: %v", err)
		}
	}
	if hasError {
		logx.Errorf("批量更新部分失败，成功: %d/%d", len(successKeys), len(items))
	}

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

		if err != nil {
			// 🔧 处理记录不存在 - 视为成功
			if strings.Contains(err.Error(), "record not found") ||
				strings.Contains(err.Error(), "no rows") {
				logx.Infof("删除目标不存在，跳过 [%s]", wrapper.Key)
				successKeys = append(successKeys, wrapper.Key)
				continue
			}

			// 🔧 处理 WHERE 条件缺失 - 这是编程错误
			if strings.Contains(err.Error(), "WHERE conditions required") {
				logx.Errorf("删除条件缺失 [%s]，需要检查 Delete 实现: %v", wrapper.Key, err)
				hasError = true

				// 回滚事务
				if rollbackErr := syncAction.Rollback(); rollbackErr != nil {
					logx.Errorf("回滚失败: %v", rollbackErr)
				}

				// 重新获取新的 syncAction
				newSyncAction := p.getDataAction(items[0].Item)
				if newSyncAction == nil {
					logx.Errorf("重新获取 syncAction 失败")
					return nil
				}

				// 降级为逐条删除
				return p.deleteItemsOneByOne(items, newSyncAction)
			}

			logx.Errorf("删除失败 [%s]: %v", wrapper.Key, err)
			hasError = true
			continue
		}

		// 删除成功
		successKeys = append(successKeys, wrapper.Key)
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

		return p.deleteItemsOneByOne(items, newSyncAction)
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

	return successKeys
}

// 🆕 逐条插入（无事务）
func (p *PrefixedBadgerDB[T]) insertItemsOneByOne(items []*SyncQueueItem[T]) []string {
	successKeys := make([]string, 0, len(items))

	for _, wrapper := range items {
		if wrapper.Item == nil {
			continue
		}
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
		if syncAfterDelete, ok := any(wrapper.Item).(ISyncAfterDelete[T]); ok {
			if needDelete := syncAfterDelete.IsSyncAfterDelete(); needDelete {
				err := p.delete(wrapper.Key, false)
				if err != nil {
					logx.Errorf("物理删除本地缓存失败 [%s]: %v", wrapper.Key, err)
				}
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
		if syncAfterDelete, ok := any(wrapper.Item).(ISyncAfterDelete[T]); ok {
			if needDelete := syncAfterDelete.IsSyncAfterDelete(); needDelete {
				err := p.delete(wrapper.Key, false)
				if err != nil {
					logx.Errorf("物理删除本地缓存失败 [%s]: %v", wrapper.Key, err)
				}
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
	close(p.closeCh)
	p.wg.Wait()

	p.manager.RemoveRef(p.prefix)

	logx.Infof("共享BadgerDB实例已关闭 [prefix=%s]", p.prefix)
	return nil
}
