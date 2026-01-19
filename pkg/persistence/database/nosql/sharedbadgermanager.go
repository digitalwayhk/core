package nosql

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/zeromicro/go-zero/core/logx"
)

// SharedBadgerManager 共享的 BadgerDB 管理器
type SharedBadgerManager struct {
	db       *badger.DB
	config   BadgerDBConfig
	mu       sync.RWMutex
	refs     map[string]int // 引用计数: prefix -> count
	closeCh  chan struct{}
	wg       sync.WaitGroup
	isClosed bool
}

var (
	globalManagers = make(map[string]*SharedBadgerManager) // basePath -> manager
	managerMutex   sync.RWMutex
)

// DefaultSharedConfig 共享模式配置（适合多个小表共享）
func DefaultSharedConfig(path string) BadgerDBConfig {
	return BadgerDBConfig{
		Path:                 path,
		Mode:                 "shared",
		MemTableSize:         128 << 20, // 128MB（比独立模式大）
		NumCompactors:        8,         // 增加 compactor
		NumLevelZeroTables:   4,
		NumLevelZeroStall:    8,
		ValueLogFileSize:     512 << 20, // 512MB（比独立模式大）
		ValueThreshold:       1024,
		SyncWrites:           false,
		DetectConflicts:      true,
		GCInterval:           10 * time.Minute,
		GCDiscardRatio:       0.5,
		EnableLogger:         false,
		PeriodicSync:         true,
		PeriodicSyncInterval: 3 * time.Second,
		AutoSync:             true,
		SyncInterval:         10 * time.Second,
		SyncMinInterval:      2 * time.Second,
		SyncMaxInterval:      5 * time.Minute,
		SyncBatchSize:        500,
		AutoCleanup:          true,
		CleanupInterval:      30 * time.Minute,
		KeepDuration:         24 * time.Hour,
		SizeThreshold:        500 * 1024 * 1024, // 500MB 触发清理
	}
}

// GetSharedManager 获取或创建共享管理器
func GetSharedManager(basePath string, config ...BadgerDBConfig) (*SharedBadgerManager, error) {
	managerMutex.Lock()
	defer managerMutex.Unlock()

	// 如果已存在，直接返回
	if manager, ok := globalManagers[basePath]; ok {
		return manager, nil
	}

	// 创建新的管理器
	var cfg BadgerDBConfig
	if len(config) > 0 {
		cfg = config[0]
	} else {
		cfg = DefaultSharedConfig(basePath)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("配置验证失败: %w", err)
	}

	// 🔧 尝试清理旧锁文件
	if cfg.Mode == "fast" || cfg.Mode == "test" {
		diagnosis := diagnoseLockError(basePath)
		logx.Infof("共享DB检查锁: %s", diagnosis)
	}

	// 构建 BadgerDB 选项（针对共享场景优化）
	opts := badger.DefaultOptions(basePath).
		WithSyncWrites(cfg.SyncWrites).
		WithDetectConflicts(cfg.DetectConflicts).
		WithNumVersionsToKeep(1).
		WithNumCompactors(cfg.NumCompactors). // 共享模式增加 compactor
		WithCompactL0OnClose(true).
		WithNumLevelZeroTables(cfg.NumLevelZeroTables).
		WithNumLevelZeroTablesStall(cfg.NumLevelZeroStall).
		WithValueLogFileSize(cfg.ValueLogFileSize). // 共享模式增大 vlog
		WithMemTableSize(cfg.MemTableSize).         // 共享模式增大内存
		WithValueThreshold(cfg.ValueThreshold)

	// 配置日志
	if cfg.EnableLogger {
		opts = opts.WithLogger(&badgerLogger{})
	} else {
		opts = opts.WithLogger(nil)
	}

	// 打开数据库（带重试）
	var db *badger.DB
	var err error
	maxRetries := 3

	for i := 0; i < maxRetries; i++ {
		db, err = badger.Open(opts)
		if err == nil {
			break
		}

		if isLockError(err) {
			diagnosis := diagnoseLockError(basePath)
			if i < maxRetries-1 {
				logx.Errorf("共享DB锁定，重试 (%d/%d): %s", i+1, maxRetries, diagnosis)
				time.Sleep(time.Second * time.Duration(i+1))
				continue
			}
			return nil, fmt.Errorf("打开共享DB失败: %s\n原始错误: %w", diagnosis, err)
		}

		return nil, fmt.Errorf("打开共享DB失败: %w", err)
	}

	manager := &SharedBadgerManager{
		db:      db,
		config:  cfg,
		refs:    make(map[string]int),
		closeCh: make(chan struct{}),
	}

	// 启动全局 GC
	manager.wg.Add(1)
	go manager.runGC()

	// 启动定期同步
	if cfg.PeriodicSync {
		manager.wg.Add(1)
		go manager.periodicSync()
	}

	globalManagers[basePath] = manager

	logx.Infof("共享BadgerDB已启动 [path=%s, mode=%s]", basePath, cfg.Mode)
	return manager, nil
}

// AddRef 增加引用计数
func (m *SharedBadgerManager) AddRef(prefix string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refs[prefix]++
	logx.Infof("共享DB添加引用 [prefix=%s, refs=%d]", prefix, m.refs[prefix])
}

// RemoveRef 减少引用计数
func (m *SharedBadgerManager) RemoveRef(prefix string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if count, ok := m.refs[prefix]; ok {
		m.refs[prefix] = count - 1
		if m.refs[prefix] <= 0 {
			delete(m.refs, prefix)
		}
		logx.Infof("共享DB移除引用 [prefix=%s, remaining=%d]", prefix, m.refs[prefix])
	}
}

// GetRefCount 获取总引用计数
func (m *SharedBadgerManager) GetRefCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.refs)
}

// periodicSync 定期同步
func (m *SharedBadgerManager) periodicSync() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.config.PeriodicSyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := m.db.Sync(); err != nil {
				logx.Errorf("共享DB同步失败: %v", err)
			}
		case <-m.closeCh:
			logx.Info("共享DB periodicSync 退出")
			return
		}
	}
}

// runGC 垃圾回收
func (m *SharedBadgerManager) runGC() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.config.GCInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			var reclaimed int
			for {
				err := m.db.RunValueLogGC(m.config.GCDiscardRatio)
				if err != nil {
					break
				}
				reclaimed++
			}
			if reclaimed > 0 {
				logx.Infof("共享DB GC完成，回收 %d 个文件", reclaimed)
			}
		case <-m.closeCh:
			logx.Info("共享DB runGC 退出")
			return
		}
	}
}

// Close 关闭共享管理器
func (m *SharedBadgerManager) Close() error {
	m.mu.Lock()
	if m.isClosed {
		m.mu.Unlock()
		return nil
	}
	m.isClosed = true
	m.mu.Unlock()

	close(m.closeCh)
	m.wg.Wait()

	if err := m.db.Sync(); err != nil {
		logx.Errorf("共享DB关闭前sync失败: %v", err)
	}

	if err := m.db.Close(); err != nil {
		return fmt.Errorf("关闭共享DB失败: %w", err)
	}

	logx.Info("共享BadgerDB已关闭")
	return nil
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

// syncBatch 批量同步（复用原有逻辑）
func (p *PrefixedBadgerDB[T]) syncBatch(items []*SyncQueueItem[T]) ([]string, error) {
	successKeys := make([]string, 0, len(items))

	p.syncLock.RLock()
	defer p.syncLock.RUnlock()

	if !p.syncDB {
		return nil, fmt.Errorf("未开启 syncDB")
	}

	for _, wrapper := range items {
		var err error
		syncAction := p.getDataAction(wrapper.Item)
		if syncAction == nil {
			logx.Errorf("未找到同步操作对象 [%s]", wrapper.Key)
			continue
		}

		setHashCode(wrapper.Item)

		switch wrapper.Op {
		case OpInsert:
			if wrapper.Item != nil {
				err = syncAction.Insert(wrapper.Item)
				if err != nil {
					if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "UNIQUE constraint failed") {
						logx.Infof("数据已存在，尝试更新操作 [%s]", wrapper.Key)
						err = nil
					}
				}
				if err == nil {
					p.updateSyncedItem(wrapper)
				}
			}
		case OpUpdate:
			if wrapper.Item != nil {
				err = syncAction.Update(wrapper.Item)
				if err == nil {
					p.updateSyncedItem(wrapper)
				}
			}
		case OpDelete:
			if wrapper.Item != nil {
				err = syncAction.Delete(wrapper.Item)
				if err == nil {
					if err1 := p.delete(wrapper.Key, false); err1 != nil {
						logx.Errorf("物理删除失败 [%s]: %v", wrapper.Key, err1)
					}
				}
			}
		}

		if err != nil {
			logx.Errorf("同步数据失败 [%s, op=%s]: %v", wrapper.Key, wrapper.Op, err)
			continue
		}

		successKeys = append(successKeys, wrapper.Key)
	}

	return successKeys, nil
}

// updateSyncedItem 更新为已同步
func (p *PrefixedBadgerDB[T]) updateSyncedItem(wrapper *SyncQueueItem[T]) error {
	return p.manager.db.Update(func(txn *badger.Txn) error {
		wrapper.IsSynced = true
		wrapper.SyncedAt = time.Now()
		wrapper.Op = OpUpdate

		data, err := json.Marshal(&wrapper)
		if err != nil {
			return err
		}

		return txn.Set([]byte(wrapper.Key), data)
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
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek([]byte(fullPrefix)); it.ValidForPrefix([]byte(fullPrefix)); it.Next() {
			count++
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

// CloseSharedManager 关闭全局共享管理器（应用退出时调用）
func CloseSharedManager(basePath string) error {
	managerMutex.Lock()
	defer managerMutex.Unlock()

	if manager, ok := globalManagers[basePath]; ok {
		delete(globalManagers, basePath)
		return manager.Close()
	}

	return nil
}

// CloseAllSharedManagers 关闭所有共享管理器
func CloseAllSharedManagers() error {
	managerMutex.Lock()
	defer managerMutex.Unlock()

	for basePath, manager := range globalManagers {
		if err := manager.Close(); err != nil {
			logx.Errorf("关闭共享管理器失败 [path=%s]: %v", basePath, err)
		}
	}

	globalManagers = make(map[string]*SharedBadgerManager)
	return nil
}
