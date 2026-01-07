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

type opType string

const (
	syncQueuePrefix         = "__sync_queue__"
	maxSyncBatchSize        = 1000
	Insert           opType = "insert"
	Update           opType = "update"
	Delete           opType = "delete"
)

// SyncQueueItem 同步队列项
type SyncQueueItem struct {
	Key       string    `json:"key"`
	Timestamp time.Time `json:"timestamp"`
	Op        opType    `json:"op"`
}

// BadgerDB 泛型 KV 数据库
type BadgerDB[T types.IModel] struct {
	db             *badger.DB
	path           string
	syncDB         types.IDataAction
	syncLock       sync.RWMutex
	syncMutex      sync.Mutex
	syncInProgress bool // 🔧 同步进行中标志
	closeCh        chan struct{}
	wg             sync.WaitGroup
	syncOnce       sync.Once
	bufferPool     sync.Pool // 🆕 添加缓冲池
}

// NewBadgerDB 创建生产环境 BadgerDB
func NewBadgerDB[T types.IModel](path string) (*BadgerDB[T], error) {
	opts := badger.DefaultOptions(path).
		WithSyncWrites(true).
		WithDetectConflicts(true).
		WithNumVersionsToKeep(1).
		WithNumCompactors(4).
		WithCompactL0OnClose(true).
		WithNumLevelZeroTables(4).
		WithNumLevelZeroTablesStall(8).
		WithValueLogFileSize(128 << 20).
		WithMemTableSize(64 << 20).
		WithValueThreshold(1024).
		WithLogger(&badgerLogger{})

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("打开 BadgerDB 失败: %w", err)
	}

	b := &BadgerDB[T]{
		db:      db,
		path:    path,
		closeCh: make(chan struct{}),
		bufferPool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, 1024) // 预分配 1KB
			},
		},
	}

	b.wg.Add(1)
	go b.runGC()

	return b, nil
}

// NewBadgerDBFast 创建快速模式 BadgerDB（牺牲持久性换取性能）
func NewBadgerDBFast[T types.IModel](path string) (*BadgerDB[T], error) {
	opts := badger.DefaultOptions(path).
		WithSyncWrites(false).
		WithDetectConflicts(false).
		WithNumVersionsToKeep(1).
		WithNumCompactors(2).
		WithCompactL0OnClose(true).
		WithNumLevelZeroTables(2).
		WithNumLevelZeroTablesStall(4).
		WithValueLogFileSize(64 << 20).
		WithMemTableSize(8 << 20).
		WithLogger(nil)

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("打开 BadgerDB 失败: %w", err)
	}

	b := &BadgerDB[T]{
		db:      db,
		path:    path,
		closeCh: make(chan struct{}),
		bufferPool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 0, 1024) // 预分配 1KB
			},
		},
	}

	b.wg.Add(2)
	go b.periodicSync()
	go b.runGC()

	return b, nil
}

// SetSyncDB 设置同步数据库
func (b *BadgerDB[T]) SetSyncDB(action types.IDataAction) {
	b.syncLock.Lock()
	defer b.syncLock.Unlock()

	if b.syncDB != nil {
		logx.Errorf("syncDB 已设置，跳过")
		return
	}

	b.syncDB = action

	if action != nil {
		// 确保只启动一次同步任务
		b.syncOnce.Do(func() {
			b.wg.Add(1)
			go b.syncToOtherDB()
		})
	}
}

// periodicSync 定期同步到磁盘
func (b *BadgerDB[T]) periodicSync() {
	defer b.wg.Done()
	ticker := time.NewTicker(1 * time.Second)
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
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			var reclaimed int
			for {
				err := b.db.RunValueLogGC(0.5)
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
func (b *BadgerDB[T]) Set(item *T, ttl time.Duration) error {
	key := b.generateKey(item)
	if key == "" {
		return badger.ErrEmptyKey
	}

	data, err := json.Marshal(item)
	if err != nil {
		return fmt.Errorf("序列化失败: %w", err)
	}

	b.syncLock.RLock()
	needSync := b.syncDB != nil
	b.syncLock.RUnlock()

	op := Insert
	if exists, _ := b.Get(key); exists != nil {
		op = Update
	}
	return b.db.Update(func(txn *badger.Txn) error {
		// 写入数据
		entry := badger.NewEntry([]byte(key), data)
		if ttl > 0 {
			entry = entry.WithTTL(ttl)
		}
		if err := txn.SetEntry(entry); err != nil {
			return err
		}

		// 添加同步标记
		if needSync {
			queueItem := SyncQueueItem{
				Key:       key,
				Timestamp: time.Now(),
				Op:        op,
			}
			queueData, err := json.Marshal(queueItem)
			if err != nil {
				return err
			}

			syncKey := fmt.Sprintf("%s%s", syncQueuePrefix, key)
			return txn.Set([]byte(syncKey), queueData)
		}

		return nil
	})
}

func (b *BadgerDB[T]) BatchInsert(items []*T) error {
	if len(items) == 0 {
		return nil
	}

	b.syncLock.RLock()
	needSync := b.syncDB != nil
	b.syncLock.RUnlock()

	// 🔧 优化 1: 批量时间戳复用
	batchTimestamp := time.Now()

	// 🔧 优化 2: 预分配序列化结果
	type serializedItem struct {
		key       string
		value     []byte
		syncKey   string
		syncValue []byte
	}

	serialized := make([]serializedItem, 0, len(items))

	// 阶段 1: 预序列化（无锁）
	for _, item := range items {
		key := b.generateKey(item)
		if key == "" {
			return badger.ErrEmptyKey
		}

		value, err := json.Marshal(item)
		if err != nil {
			return fmt.Errorf("序列化失败: %w", err)
		}

		si := serializedItem{
			key:   key,
			value: value,
		}

		if needSync {
			// 🔧 优化 3: 使用批量时间戳
			queueItem := SyncQueueItem{
				Key:       key,
				Timestamp: batchTimestamp,
			}
			queueData, _ := json.Marshal(queueItem)
			si.syncKey = fmt.Sprintf("%s%s", syncQueuePrefix, key)
			si.syncValue = queueData
		}

		serialized = append(serialized, si)
	}

	// 阶段 2: 批量写入事务（快速）
	const maxRetries = 3
	var lastErr error

	for retry := 0; retry < maxRetries; retry++ {
		txn := b.db.NewTransaction(true)
		success := true

		for _, si := range serialized {
			if err := b.setInTxn(txn, si.key, si.value); err != nil {
				if err == badger.ErrTxnTooBig {
					if commitErr := txn.Commit(); commitErr != nil {
						lastErr = commitErr
						success = false
						break
					}
					txn = b.db.NewTransaction(true)
					if err := b.setInTxn(txn, si.key, si.value); err != nil {
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

			if needSync && si.syncKey != "" {
				if err := b.setInTxn(txn, si.syncKey, si.syncValue); err != nil {
					if err == badger.ErrTxnTooBig {
						if commitErr := txn.Commit(); commitErr != nil {
							lastErr = commitErr
							success = false
							break
						}
						txn = b.db.NewTransaction(true)
						if err := b.setInTxn(txn, si.syncKey, si.syncValue); err != nil {
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

// setInTxn 在事务中设置键值
func (b *BadgerDB[T]) setInTxn(txn *badger.Txn, key string, value []byte) error {
	return txn.Set([]byte(key), value)
}

// Delete 删除数据
func (b *BadgerDB[T]) Delete(key string) error {
	return b.db.Update(func(txn *badger.Txn) error {
		if err := txn.Delete([]byte(key)); err != nil && err != badger.ErrKeyNotFound {
			return err
		}

		syncKey := fmt.Sprintf("%s%s", syncQueuePrefix, key)
		txn.Delete([]byte(syncKey))

		return nil
	})
}

// Get 获取数据
func (b *BadgerDB[T]) Get(key string) (*T, error) {
	var result = new(T)
	if hook, ok := any(result).(types.IModelNewHook); ok {
		hook.NewModel()
	}

	err := b.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}

		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, result)
		})
	})

	return result, err
}

// Scan 扫描数据
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
			key := string(item.Key())

			if isInternalKey(key) {
				continue
			}

			err := item.Value(func(val []byte) error {
				var data = new(T)
				if hook, ok := any(data).(types.IModelNewHook); ok {
					hook.NewModel()
				}
				if err := json.Unmarshal(val, data); err != nil {
					return err
				}
				results = append(results, data)
				return nil
			})

			if err != nil {
				logx.Errorf("解析数据失败 [%s]: %v", key, err)
				continue
			}
			count++
		}
		return nil
	})

	return results, err
}

// isInternalKey 判断是否为内部键
func isInternalKey(key string) bool {
	return len(key) >= len(syncQueuePrefix) && key[:len(syncQueuePrefix)] == syncQueuePrefix
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

// syncToOtherDB 同步到其他数据库
func (b *BadgerDB[T]) syncToOtherDB() {
	defer b.wg.Done()

	// 🔧 动态间隔：初始 1 秒，根据同步耗时调整
	interval := 1 * time.Second
	minInterval := 1 * time.Second
	maxInterval := 10 * time.Second

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

			// 🔧 检查是否正在同步
			b.syncMutex.Lock()
			if b.syncInProgress {
				b.syncMutex.Unlock()
				logx.Errorf("上次同步未完成，跳过本次")

				// 🔧 延长间隔时间
				interval = min(interval*2, maxInterval)
				ticker.Reset(interval)
				continue
			}
			b.syncInProgress = true
			b.syncMutex.Unlock()

			// 🔧 记录同步开始时间
			start := time.Now()

			// 执行同步
			if err := b.processSyncQueue(); err != nil {
				logx.Errorf("同步到其他DB失败: %v", err)
			}

			// 🔧 记录同步耗时并调整间隔
			duration := time.Since(start)

			b.syncMutex.Lock()
			b.syncInProgress = false
			b.syncMutex.Unlock()

			// 🔧 根据耗时动态调整
			if duration < interval/2 {
				// 同步很快，缩短间隔
				interval = max(interval/2, minInterval)
			} else if duration > interval {
				// 同步较慢，延长间隔
				interval = min(duration*2, maxInterval)
			}

			ticker.Reset(interval)
			logx.Infof("同步完成，耗时 %v，下次间隔 %v", duration, interval)

		case <-b.closeCh:
			// 🔧 等待最后一次同步完成
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

// min/max 辅助函数（Go 1.21+）
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

// processSyncQueue 处理同步队列
func (b *BadgerDB[T]) processSyncQueue() error {
	// 获取队列（不持锁）
	syncItems, err := b.getSyncQueueBatch(maxSyncBatchSize)
	if err != nil {
		return fmt.Errorf("获取同步队列失败: %w", err)
	}

	if len(syncItems) == 0 {
		return nil
	}

	logx.Infof("开始同步 %d 条数据到其他DB", len(syncItems))

	// 同步数据（不持锁）
	successKeys, err := b.syncBatch(syncItems)
	if err != nil {
		logx.Errorf("批量同步失败: %v", err)
	}

	// 删除标记
	if len(successKeys) > 0 {
		if err := b.removeSyncMarks(successKeys); err != nil {
			logx.Errorf("删除同步标记失败: %v", err)
		} else {
			logx.Infof("成功同步并清理 %d 条数据标记", len(successKeys))
		}
	}

	return nil
}

// getSyncQueueBatch 分批获取同步队列
func (b *BadgerDB[T]) getSyncQueueBatch(limit int) ([]SyncQueueItem, error) {
	var items []SyncQueueItem

	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 100
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		prefix := []byte(syncQueuePrefix)
		count := 0

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			if count >= limit {
				break
			}

			item := it.Item()

			err := item.Value(func(val []byte) error {
				var queueItem SyncQueueItem
				if err := json.Unmarshal(val, &queueItem); err != nil {
					return err
				}
				items = append(items, queueItem)
				count++
				return nil
			})

			if err != nil {
				logx.Errorf("解析同步队列项失败: %v", err)
				continue
			}
		}
		return nil
	})

	return items, err
}

// syncBatch 批量同步数据
func (b *BadgerDB[T]) syncBatch(items []SyncQueueItem) ([]string, error) {
	successKeys := make([]string, 0, len(items))

	b.syncLock.RLock()
	b.syncDB.Transaction()
	b.syncLock.RUnlock()

	defer func() {
		if r := recover(); r != nil {
			logx.Errorf("同步 panic: %v", r)
		}
	}()

	for _, queueItem := range items {
		data, err := b.Get(queueItem.Key)
		if err == badger.ErrKeyNotFound {
			// 数据已删除，标记为成功
			successKeys = append(successKeys, queueItem.Key)
			continue
		}

		if err != nil {
			logx.Errorf("读取数据失败 [%s]: %v", queueItem.Key, err)
			continue
		}
		if queueItem.Op == Insert {
			if err := b.syncDB.Insert(data); err != nil {
				logx.Errorf("同步数据失败 [%s]: %v", queueItem.Key, err)
				continue
			}
		}
		if queueItem.Op == Update {
			if err := b.syncDB.Update(data); err != nil {
				logx.Errorf("同步数据失败 [%s]: %v", queueItem.Key, err)
				continue
			}
		}
		successKeys = append(successKeys, queueItem.Key)
	}

	if err := b.syncDB.Commit(); err != nil {
		return nil, fmt.Errorf("提交同步事务失败: %w", err)
	}

	return successKeys, nil
}

// removeSyncMarks 删除同步标记
func (b *BadgerDB[T]) removeSyncMarks(keys []string) error {
	return b.db.Update(func(txn *badger.Txn) error {
		for _, key := range keys {
			syncKey := fmt.Sprintf("%s%s", syncQueuePrefix, key)
			if err := txn.Delete([]byte(syncKey)); err != nil && err != badger.ErrKeyNotFound {
				logx.Errorf("删除同步标记失败 [%s]: %v", key, err)
			}
		}
		return nil
	})
}

// GetPendingSyncCount 获取待同步数量
func (b *BadgerDB[T]) GetPendingSyncCount() (int, error) {
	count := 0

	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()

		prefix := []byte(syncQueuePrefix)
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			count++
		}
		return nil
	})

	return count, err
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

// GetAll 获取所有数据
func (b *BadgerDB[T]) GetAll() ([]*T, error) {
	var results []*T

	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := string(item.Key())

			if isInternalKey(key) {
				continue
			}

			err := item.Value(func(val []byte) error {
				var data = new(T)
				if hook, ok := any(data).(types.IModelNewHook); ok {
					hook.NewModel()
				}
				if err := json.Unmarshal(val, data); err != nil {
					return err
				}
				results = append(results, data)
				return nil
			})

			if err != nil {
				logx.Errorf("解析数据失败 [%s]: %v", key, err)
				continue
			}
		}
		return nil
	})

	return results, err
}

// GetStats 获取数据库统计信息
func (b *BadgerDB[T]) GetStats() string {
	lsm, vlog := b.db.Size()
	return fmt.Sprintf("LSM 大小: %d MB, VLog 大小: %d MB", lsm/(1024*1024), vlog/(1024*1024))
}
