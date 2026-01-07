package nosql

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/zeromicro/go-zero/core/logx"
)

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

// BadgerDB 泛型 KV 数据库
type BadgerDB[T types.IModel] struct {
	db             *badger.DB
	path           string
	config         BadgerDBConfig // 🆕 配置
	syncDB         types.IDataAction
	syncLock       sync.RWMutex
	syncMutex      sync.Mutex
	syncInProgress bool
	closeCh        chan struct{}
	wg             sync.WaitGroup
	syncOnce       sync.Once
	cleanupOnce    sync.Once // 🆕 清理启动控制
	bufferPool     sync.Pool
}

// NewBadgerDB 创建生产环境 BadgerDB（保持向后兼容）
func NewBadgerDB[T types.IModel](path string) (*BadgerDB[T], error) {
	config := DefaultProductionConfig(path)
	return NewBadgerDBWithConfig[T](config)
}

// NewBadgerDBFast 创建快速模式 BadgerDB（保持向后兼容）
func NewBadgerDBFast[T types.IModel](path string) (*BadgerDB[T], error) {
	config := DefaultFastConfig(path)
	config.PeriodicSync = true
	return NewBadgerDBWithConfig[T](config)
}

// NewBadgerDBWithConfig 使用配置创建 BadgerDB
func NewBadgerDBWithConfig[T types.IModel](config BadgerDBConfig) (*BadgerDB[T], error) {
	// 验证配置
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("配置验证失败: %w", err)
	}

	// 构建 BadgerDB 选项
	opts := badger.DefaultOptions(config.Path).
		WithSyncWrites(config.SyncWrites).
		WithDetectConflicts(config.DetectConflicts).
		WithNumVersionsToKeep(1).
		WithNumCompactors(config.NumCompactors).
		WithCompactL0OnClose(true).
		WithNumLevelZeroTables(config.NumLevelZeroTables).
		WithNumLevelZeroTablesStall(config.NumLevelZeroStall).
		WithValueLogFileSize(config.ValueLogFileSize).
		WithMemTableSize(config.MemTableSize).
		WithValueThreshold(config.ValueThreshold)

	// 配置日志
	if config.EnableLogger {
		opts = opts.WithLogger(&badgerLogger{})
	} else {
		opts = opts.WithLogger(nil)
	}

	// 打开数据库
	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("打开 BadgerDB 失败: %w", err)
	}

	b := &BadgerDB[T]{
		db:      db,
		path:    config.Path,
		config:  config,
		closeCh: make(chan struct{}),
		bufferPool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, 1024)
			},
		},
	}

	// 启动 GC
	b.wg.Add(1)
	go b.runGC()

	// 启动定期磁盘同步（Fast 模式）
	if config.PeriodicSync {
		b.wg.Add(1)
		go b.periodicSync()
	}

	logx.Infof("BadgerDB 已启动 [mode=%s, path=%s, autoSync=%v, autoCleanup=%v]",
		config.Mode, config.Path, config.AutoSync, config.AutoCleanup)

	return b, nil
}

// SetSyncDB 设置同步数据库
func (b *BadgerDB[T]) SetSyncDB(action types.IDataAction) {
	b.syncLock.Lock()
	defer b.syncLock.Unlock()

	if b.syncDB != nil {
		logx.Error("syncDB 已设置，跳过")
		return
	}

	b.syncDB = action

	if action != nil {
		// 🔧 启动自动同步
		if b.config.AutoSync {
			b.syncOnce.Do(func() {
				b.wg.Add(1)
				go b.syncToOtherDB()
				logx.Info("自动同步已启动")
			})
		}

		// 🔧 启动自动清理
		if b.config.AutoCleanup {
			b.cleanupOnce.Do(func() {
				b.wg.Add(1)
				go b.autoCleanup()
				logx.Info("自动清理已启动")
			})
		}
	}
}

// generateKey 生成 key
func (b *BadgerDB[T]) generateKey(item *T) string {
	if item == nil {
		return ""
	}
	if rowCode, ok := any(item).(types.IRowCode); ok {
		return rowCode.GetHash()
	}
	return ""
}

// Set 写入数据
func (b *BadgerDB[T]) Set(item *T, ttl time.Duration, fn ...func(wrapper *SyncQueueItem[T])) error {
	if item == nil {
		return fmt.Errorf("item 不能为空")
	}

	key := b.generateKey(item)
	if key == "" {
		return badger.ErrEmptyKey
	}

	b.syncLock.RLock()
	needSync := b.syncDB != nil
	b.syncLock.RUnlock()

	// 🔧 检查是插入还是更新
	op := OpInsert
	existingWrapper, err := b.getWrapper(key)
	if err == nil && existingWrapper != nil && !existingWrapper.IsDeleted {
		op = OpUpdate
	}

	// 🔧 创建包装对象
	now := time.Now()
	wrapper := &SyncQueueItem[T]{
		Key:       key,
		Item:      item,
		Op:        op,
		CreatedAt: now,
		UpdatedAt: now,
		IsSynced:  !needSync,
		IsDeleted: false,
	}

	// 🔧 保留创建时间（如果是更新）
	if op == OpUpdate && existingWrapper != nil {
		wrapper.CreatedAt = existingWrapper.CreatedAt
	}

	// 序列化
	data, err := json.Marshal(wrapper)
	if err != nil {
		return fmt.Errorf("序列化失败: %w", err)
	}
	if len(fn) > 0 {
		fn[0](wrapper)
	}
	// 写入数据库
	return b.db.Update(func(txn *badger.Txn) error {
		entry := badger.NewEntry([]byte(key), data)
		if ttl > 0 {
			entry = entry.WithTTL(ttl)
		}
		return txn.SetEntry(entry)
	})
}

// BatchInsert 批量插入
func (b *BadgerDB[T]) BatchInsert(items []*T) error {
	if len(items) == 0 {
		return nil
	}

	b.syncLock.RLock()
	needSync := b.syncDB != nil
	b.syncLock.RUnlock()

	now := time.Now()

	type serializedItem struct {
		key   string
		value []byte
	}

	serialized := make([]serializedItem, 0, len(items))

	for _, item := range items {
		if item == nil {
			continue
		}

		key := b.generateKey(item)
		if key == "" {
			return badger.ErrEmptyKey
		}

		// 🔧 创建包装对象
		wrapper := &SyncQueueItem[T]{
			Key:       key,
			Item:      item,
			Op:        OpInsert,
			CreatedAt: now,
			UpdatedAt: now,
			IsSynced:  !needSync,
			IsDeleted: false,
		}

		value, err := json.Marshal(wrapper)
		if err != nil {
			return fmt.Errorf("序列化失败: %w", err)
		}

		serialized = append(serialized, serializedItem{
			key:   key,
			value: value,
		})
	}

	// 批量写入
	const maxRetries = 3
	var lastErr error

	for retry := 0; retry < maxRetries; retry++ {
		txn := b.db.NewTransaction(true)
		success := true

		for _, si := range serialized {
			if err := txn.Set([]byte(si.key), si.value); err != nil {
				if err == badger.ErrTxnTooBig {
					if commitErr := txn.Commit(); commitErr != nil {
						lastErr = commitErr
						success = false
						break
					}
					txn = b.db.NewTransaction(true)
					if err := txn.Set([]byte(si.key), si.value); err != nil {
						txn.Discard()
						lastErr = err
						success = false
						break
					}
				} else {
					txn.Discard()
					lastErr = err
					success = false
					break
				}
			}
		}

		if success {
			if err := txn.Commit(); err != nil {
				lastErr = err
				time.Sleep(time.Millisecond * 100 * time.Duration(retry+1))
				continue
			}
			return nil
		}

		txn.Discard()
		time.Sleep(time.Millisecond * 100 * time.Duration(retry+1))
	}

	return lastErr
}

// Delete 删除数据（支持软删除）
func (b *BadgerDB[T]) Delete(key string) error {
	b.syncLock.RLock()
	needSync := b.syncDB != nil
	b.syncLock.RUnlock()

	if !needSync {
		// 🔧 不需要同步，直接物理删除
		return b.db.Update(func(txn *badger.Txn) error {
			return txn.Delete([]byte(key))
		})
	}

	// 🔧 需要同步，执行软删除
	return b.db.Update(func(txn *badger.Txn) error {
		// 读取现有数据
		item, err := txn.Get([]byte(key))
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return nil // 数据不存在，直接返回
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

		// 🔧 如果已经是删除状态，直接返回
		if wrapper.IsDeleted {
			return nil
		}

		// 🔧 标记为软删除
		now := time.Now()
		wrapper.Op = OpDelete
		wrapper.IsDeleted = true
		wrapper.DeletedAt = now
		wrapper.UpdatedAt = now
		wrapper.IsSynced = false // 需要同步删除操作

		// 写回
		data, err := json.Marshal(&wrapper)
		if err != nil {
			return fmt.Errorf("序列化失败: %w", err)
		}

		return txn.Set([]byte(key), data)
	})
}

// Get 获取数据（过滤已删除的数据）
func (b *BadgerDB[T]) Get(key string) (*T, error) {
	wrapper, err := b.getWrapper(key)
	if err != nil {
		return nil, err
	}

	// 🔧 过滤已删除的数据
	if wrapper.IsDeleted {
		return nil, badger.ErrKeyNotFound
	}

	return wrapper.Item, nil
}

// getWrapper 获取包装对象（内部使用）
func (b *BadgerDB[T]) getWrapper(key string) (*SyncQueueItem[T], error) {
	var wrapper = new(SyncQueueItem[T])

	err := b.db.View(func(txn *badger.Txn) error {
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

	// 🔧 初始化 Item
	if wrapper.Item != nil {
		if hook, ok := any(wrapper.Item).(types.IModelNewHook); ok {
			hook.NewModel()
		}
	}

	return wrapper, nil
}

// Scan 扫描数据（过滤已删除的数据）
func (b *BadgerDB[T]) Scan(prefix string, limit int) ([]*T, error) {
	var results []*T

	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 100
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		count := 0
		for it.Seek([]byte(prefix)); it.ValidForPrefix([]byte(prefix)); it.Next() {
			if count >= limit {
				break
			}

			item := it.Item()

			err := item.Value(func(val []byte) error {
				var wrapper SyncQueueItem[T]
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return err
				}

				// 🔧 过滤已删除的数据
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

// GetAll 获取所有数据（过滤已删除的数据）
func (b *BadgerDB[T]) GetAll() ([]*T, error) {
	var results []*T

	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()

			err := item.Value(func(val []byte) error {
				var wrapper SyncQueueItem[T]
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return err
				}

				// 🔧 过滤已删除的数据
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

// GetPendingSyncCount 获取待同步数量
func (b *BadgerDB[T]) GetPendingSyncCount() (int, error) {
	count := 0

	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()

			err := item.Value(func(val []byte) error {
				var wrapper SyncQueueItem[T]
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return err
				}

				// 🔧 统计未同步的数据（包括删除操作）
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

// processSyncQueue 处理同步队列
func (b *BadgerDB[T]) processSyncQueue() error {
	// 🔧 使用配置中的批次大小
	unsyncedItems, err := b.getUnsyncedBatch(b.config.SyncBatchSize)
	if err != nil {
		return fmt.Errorf("获取未同步数据失败: %w", err)
	}

	if len(unsyncedItems) == 0 {
		return nil
	}

	logx.Infof("开始同步 %d 条数据到其他DB", len(unsyncedItems))

	successKeys, err := b.syncBatch(unsyncedItems)
	if err != nil {
		logx.Errorf("批量同步失败: %v", err)
	}

	if len(successKeys) > 0 {
		if err := b.handleSyncedItems(successKeys); err != nil {
			logx.Errorf("处理已同步数据失败: %v", err)
		} else {
			logx.Infof("成功同步 %d 条数据", len(successKeys))
		}
	}

	return nil
}

// getUnsyncedBatch 获取未同步的数据
func (b *BadgerDB[T]) getUnsyncedBatch(limit int) ([]*SyncQueueItem[T], error) {
	var items []*SyncQueueItem[T]

	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		count := 0
		for it.Rewind(); it.Valid(); it.Next() {
			if count >= limit {
				break
			}

			item := it.Item()

			err := item.Value(func(val []byte) error {
				var wrapper SyncQueueItem[T]
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return err
				}

				// 🔧 只返回未同步的数据（包括删除操作）
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

// syncBatch 批量同步数据
func (b *BadgerDB[T]) syncBatch(items []*SyncQueueItem[T]) ([]string, error) {
	successKeys := make([]string, 0, len(items))

	b.syncLock.RLock()
	defer b.syncLock.RUnlock()

	if b.syncDB == nil {
		return nil, fmt.Errorf("syncDB 未配置")
	}

	b.syncDB.Transaction()
	defer func() {
		if r := recover(); r != nil {
			logx.Errorf("同步 panic: %v", r)
		}
	}()

	for _, wrapper := range items {
		var err error

		switch wrapper.Op {
		case OpInsert:
			if wrapper.Item != nil {
				err = b.syncDB.Insert(wrapper.Item)
			}
		case OpUpdate:
			if wrapper.Item != nil {
				err = b.syncDB.Update(wrapper.Item)
			}
		case OpDelete:
			// 🔧 同步删除操作
			if wrapper.Item != nil {
				err = b.syncDB.Delete(wrapper.Item)
			}
		default:
			logx.Errorf("未知操作类型: %s", wrapper.Op)
			continue
		}

		if err != nil {
			logx.Errorf("同步数据失败 [%s, op=%s]: %v", wrapper.Key, wrapper.Op, err)
			continue
		}

		successKeys = append(successKeys, wrapper.Key)
	}

	if err := b.syncDB.Commit(); err != nil {
		return nil, fmt.Errorf("提交同步事务失败: %w", err)
	}

	return successKeys, nil
}

// handleSyncedItems 处理已同步的数据
func (b *BadgerDB[T]) handleSyncedItems(keys []string) error {
	return b.db.Update(func(txn *badger.Txn) error {
		for _, key := range keys {
			item, err := txn.Get([]byte(key))
			if err != nil {
				if err == badger.ErrKeyNotFound {
					continue
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

			// 🔧 如果是删除操作，物理删除
			if wrapper.Op == OpDelete && wrapper.IsDeleted {
				if err := txn.Delete([]byte(key)); err != nil {
					logx.Errorf("物理删除失败 [%s]: %v", key, err)
				}
				continue
			}

			// 🔧 否则，标记为已同步
			wrapper.IsSynced = true
			wrapper.SyncedAt = time.Now()

			data, err := json.Marshal(&wrapper)
			if err != nil {
				return err
			}

			if err := txn.Set([]byte(key), data); err != nil {
				return err
			}
		}
		return nil
	})
}

// ManualSync 手动触发同步
func (b *BadgerDB[T]) ManualSync() error {
	b.syncLock.RLock()
	hasDB := b.syncDB != nil
	b.syncLock.RUnlock()

	if !hasDB {
		return fmt.Errorf("syncDB 未配置")
	}

	return b.processSyncQueue()
}

// CleanupAfterSync 清理已同步的数据
func (b *BadgerDB[T]) CleanupAfterSync(keepDuration time.Duration) error {
	count := 0
	deletedCount := 0
	cutoffTime := time.Now().Add(-keepDuration)

	err := b.db.Update(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := item.Key()

			err := item.Value(func(val []byte) error {
				var wrapper SyncQueueItem[T]
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return err
				}

				count++

				// 🔧 清理已同步且超过保留时间的数据
				if wrapper.IsSynced && !wrapper.SyncedAt.IsZero() && wrapper.SyncedAt.Before(cutoffTime) {
					if err := txn.Delete(key); err != nil {
						return err
					}
					deletedCount++
				}

				return nil
			})

			if err != nil {
				logx.Errorf("清理数据失败: %v", err)
			}
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("清理失败: %w", err)
	}

	logx.Infof("清理完成: 检查 %d 条，删除 %d 条，保留 %d 条", count, deletedCount, count-deletedCount)
	return nil
}

func (b *BadgerDB[T]) periodicSync() {
	defer b.wg.Done()

	// 🔧 使用配置中的间隔
	ticker := time.NewTicker(b.config.PeriodicSyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := b.db.Sync(); err != nil {
				logx.Errorf("BadgerDB sync 失败: %v", err)
			}
		case <-b.closeCh:
			logx.Info("periodicSync 退出")
			return
		}
	}
}

// runGC 垃圾回收
func (b *BadgerDB[T]) runGC() {
	defer b.wg.Done()

	// 🔧 使用配置中的 GC 间隔
	ticker := time.NewTicker(b.config.GCInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			var reclaimed int
			for {
				err := b.db.RunValueLogGC(b.config.GCDiscardRatio)
				if err != nil {
					break
				}
				reclaimed++
			}
			if reclaimed > 0 {
				logx.Infof("GC 完成，回收 %d 个文件", reclaimed)
			}
		case <-b.closeCh:
			logx.Info("runGC 退出")
			return
		}
	}
}

// Close 关闭数据库
func (b *BadgerDB[T]) Close() error {
	close(b.closeCh)
	b.wg.Wait()

	if err := b.db.Sync(); err != nil {
		logx.Errorf("关闭前 sync 失败: %v", err)
	}

	if err := b.db.Close(); err != nil {
		return fmt.Errorf("关闭 BadgerDB 失败: %w", err)
	}

	logx.Info("BadgerDB 已关闭")
	return nil
}

// badgerLogger 日志适配器
type badgerLogger struct{}

func (l *badgerLogger) Errorf(f string, v ...interface{})   { logx.Errorf(f, v...) }
func (l *badgerLogger) Warningf(f string, v ...interface{}) { logx.Infof(f, v...) }
func (l *badgerLogger) Infof(f string, v ...interface{})    { logx.Infof(f, v...) }
func (l *badgerLogger) Debugf(f string, v ...interface{})   {}

// syncToOtherDB 同步到其他数据库
func (b *BadgerDB[T]) syncToOtherDB() {
	defer b.wg.Done()

	// 🔧 使用配置中的间隔参数
	interval := b.config.SyncInterval
	minInterval := b.config.SyncMinInterval
	maxInterval := b.config.SyncMaxInterval

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			b.syncLock.RLock()
			hasDB := b.syncDB != nil
			b.syncLock.RUnlock()

			if !hasDB {
				continue
			}

			b.syncMutex.Lock()
			if b.syncInProgress {
				b.syncMutex.Unlock()
				logx.Info("上次同步未完成，跳过本次")
				interval = min(interval*2, maxInterval)
				ticker.Reset(interval)
				continue
			}
			b.syncInProgress = true
			b.syncMutex.Unlock()

			start := time.Now()

			if err := b.processSyncQueue(); err != nil {
				logx.Errorf("同步到其他DB失败: %v", err)
			}

			duration := time.Since(start)

			b.syncMutex.Lock()
			b.syncInProgress = false
			b.syncMutex.Unlock()

			// 🔧 自适应调整间隔
			if duration < interval/2 {
				interval = max(interval/2, minInterval)
			} else if duration > interval {
				interval = min(duration*2, maxInterval)
			}

			ticker.Reset(interval)
			logx.Infof("同步完成，耗时 %v，下次间隔 %v", duration, interval)

		case <-b.closeCh:
			b.syncMutex.Lock()
			for b.syncInProgress {
				b.syncMutex.Unlock()
				time.Sleep(100 * time.Millisecond)
				b.syncMutex.Lock()
			}
			b.syncMutex.Unlock()

			logx.Info("syncToOtherDB 退出")
			return
		}
	}
}

// autoCleanup 自动清理（新方法，使用配置）
func (b *BadgerDB[T]) autoCleanup() {
	defer b.wg.Done()

	ticker := time.NewTicker(b.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 🔧 改进：支持多种清理触发条件
			shouldCleanup := false
			cleanupReason := ""

			// 条件 1：检查文件大小
			if b.config.SizeThreshold > 0 {
				// 先刷盘
				if err := b.db.Sync(); err != nil {
					logx.Errorf("同步到磁盘失败: %v", err)
				}

				lsm, vlog, err := b.GetDBSize()
				if err != nil {
					logx.Errorf("获取数据库大小失败: %v", err)
				} else {
					totalSize := lsm + vlog
					logx.Infof("数据库大小: LSM=%dMB, VLog=%dMB, Total=%dMB",
						lsm/(1024*1024), vlog/(1024*1024), totalSize/(1024*1024))

					if totalSize >= b.config.SizeThreshold {
						shouldCleanup = true
						cleanupReason = fmt.Sprintf("文件大小超过阈值 (%dMB)", b.config.SizeThreshold/(1024*1024))
					}
				}
			}

			// 🆕 条件 2：检查已同步的旧数据（适合小数据量场景）
			if !shouldCleanup {
				syncedCount, err := b.countSyncedOldData()
				if err != nil {
					logx.Errorf("统计已同步旧数据失败: %v", err)
				} else if syncedCount > 0 {
					shouldCleanup = true
					cleanupReason = fmt.Sprintf("发现 %d 条已同步的旧数据", syncedCount)
				}
			}

			// 🆕 条件 3：定期强制清理（当 SizeThreshold=0 时）
			if !shouldCleanup && b.config.SizeThreshold == 0 {
				// 强制定期清理
				shouldCleanup = true
				cleanupReason = "定期清理（SizeThreshold=0）"
			}

			if !shouldCleanup {
				continue
			}

			logx.Infof("触发清理: %s", cleanupReason)

			// 🔧 恢复：先确保数据同步完成
			// if err := b.ManualSync(); err != nil {
			// 	logx.Errorf("同步失败: %v", err)
			// 	continue
			// }

			//time.Sleep(500 * time.Millisecond)

			// 清理已同步的数据
			if err := b.CleanupAfterSync(b.config.KeepDuration); err != nil {
				logx.Errorf("清理失败: %v", err)
				continue
			}

			// GC
			var reclaimed int
			for {
				err := b.db.RunValueLogGC(b.config.GCDiscardRatio)
				if err != nil {
					break
				}
				reclaimed++
			}

			// 再次刷盘
			if err := b.db.Sync(); err != nil {
				logx.Errorf("清理后同步失败: %v", err)
			}

			// 统计清理效果
			lsmAfter, vlogAfter, _ := b.GetDBSize()
			totalAfter := lsmAfter + vlogAfter

			if reclaimed > 0 {
				logx.Infof("清理完成，回收 %d 个文件，当前大小 %dMB", reclaimed, totalAfter/(1024*1024))
			} else {
				logx.Infof("清理完成，当前大小 %dMB", totalAfter/(1024*1024))
			}

		case <-b.closeCh:
			logx.Info("autoCleanup 退出")
			return
		}
	}
}

// 🆕 统计已同步且超过保留期限的数据数量
func (b *BadgerDB[T]) countSyncedOldData() (int, error) {
	count := 0
	cutoffTime := time.Now().Add(-b.config.KeepDuration)

	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		opts.PrefetchSize = 10 // 只预取少量数据
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()

			err := item.Value(func(val []byte) error {
				var wrapper SyncQueueItem[T]
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return nil // 忽略解析错误
				}

				// 🔧 修复：检查三个条件
				if wrapper.IsSynced && !wrapper.SyncedAt.IsZero() && wrapper.SyncedAt.Before(cutoffTime) {
					count++
				}

				return nil
			})

			if err != nil {
				continue
			}

			// 提前退出（只需要知道有没有需要清理的数据）
			if count > 0 {
				break
			}
		}
		return nil
	})

	return count, err
}

// min/max 辅助函数
func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}

func max(a, b time.Duration) time.Duration {
	if a > b {
		return a
	}
	return b
}

// DropAll 删除所有数据（危险操作）
func (b *BadgerDB[T]) DropAll() error {
	return b.db.DropAll()
}

// GetDBSize 获取数据库大小
func (b *BadgerDB[T]) GetDBSize() (int64, int64, error) {
	lsm, vlog := b.db.Size()
	return lsm, vlog, nil
}

// GetStats 获取数据库统计信息
func (b *BadgerDB[T]) GetStats() string {
	lsm, vlog := b.db.Size()
	return fmt.Sprintf("LSM 大小: %d MB, VLog 大小: %d MB", lsm/(1024*1024), vlog/(1024*1024))
}

// Sync 同步到磁盘
func (b *BadgerDB[T]) Sync() error {
	return b.db.Sync()
}

// Backup 备份数据库
func (b *BadgerDB[T]) Backup(backupPath string) error {
	f, err := os.Create(backupPath)
	if err != nil {
		return fmt.Errorf("创建备份文件失败: %w", err)
	}
	defer f.Close()

	_, err = b.db.Backup(f, 0)
	if err != nil {
		return fmt.Errorf("备份失败: %w", err)
	}

	logx.Infof("备份成功: %s", backupPath)
	return nil
}

// SafeInsert 安全插入（立即同步到磁盘）
func (b *BadgerDB[T]) SafeInsert(data *T) error {
	if err := b.Set(data, 0); err != nil {
		return err
	}

	if err := b.Sync(); err != nil {
		logx.Errorf("同步失败: %v", err)
	}

	return nil
}

// GetConfig 获取当前配置
func (b *BadgerDB[T]) GetConfig() BadgerDBConfig {
	return b.config
}

// UpdateConfig 更新配置（部分参数）
func (b *BadgerDB[T]) UpdateConfig(updateFn func(*BadgerDBConfig)) error {
	b.syncLock.Lock()
	defer b.syncLock.Unlock()

	oldConfig := b.config
	updateFn(&b.config)

	if err := b.config.Validate(); err != nil {
		b.config = oldConfig
		return fmt.Errorf("配置更新失败: %w", err)
	}

	logx.Infof("配置已更新: %+v", b.config)
	return nil
}
