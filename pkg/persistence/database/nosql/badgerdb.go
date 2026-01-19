package nosql

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
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
	syncDB         bool
	syncList       *entity.ModelList[T]
	syncLock       sync.RWMutex
	syncMutex      sync.Mutex
	syncInProgress bool
	closeCh        chan struct{}
	wg             sync.WaitGroup
	syncOnce       sync.Once
	cleanupOnce    sync.Once // 🆕 清理启动控制
	bufferPool     sync.Pool
	isAutoClean    bool // 🆕 是否启用自动清理

	// 🆕 待同步计数缓存
	pendingCountCache int
	pendingCountMutex sync.RWMutex
	lastCountUpdate   time.Time
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

// 🆕 检查并诊断锁定错误
func diagnoseLockError(dbPath string) string {
	lockFile := filepath.Join(dbPath, "LOCK")

	// 检查锁文件是否存在
	if _, err := os.Stat(lockFile); os.IsNotExist(err) {
		return "锁文件不存在（可能是其他错误）"
	}

	// 尝试读取锁文件内容（BadgerDB 会写入进程信息）
	content, err := os.ReadFile(lockFile)
	if err != nil {
		return fmt.Sprintf("无法读取锁文件: %v", err)
	}

	// 解析锁文件内容
	lines := strings.Split(string(content), "\n")
	if len(lines) > 0 && lines[0] != "" {
		pid, err := strconv.Atoi(strings.TrimSpace(lines[0]))
		if err == nil {
			// 检查进程是否还在运行
			process, err := os.FindProcess(pid)
			if err != nil {
				return fmt.Sprintf("锁定进程 PID=%d 已不存在（可能是僵尸锁）", pid)
			}

			// 尝试发送信号 0 检查进程是否存活
			err = process.Signal(syscall.Signal(0))
			if err != nil {
				return fmt.Sprintf("锁定进程 PID=%d 已不存在（僵尸锁），建议手动删除锁文件", pid)
			}

			// 🔧 获取进程信息（macOS/Linux）
			processInfo := getProcessInfo(pid)
			return fmt.Sprintf("数据库被进程锁定 [PID=%d, %s]", pid, processInfo)
		}
	}

	return fmt.Sprintf("锁文件存在但格式异常: %s", string(content))
}

// 🆕 获取进程信息
func getProcessInfo(pid int) string {
	// macOS: 使用 ps 命令
	cmdPath := fmt.Sprintf("/proc/%d/cmdline", pid)
	if content, err := os.ReadFile(cmdPath); err == nil {
		cmd := strings.ReplaceAll(string(content), "\x00", " ")
		return fmt.Sprintf("命令: %s", cmd)
	}

	// 备用方案：只返回 PID
	return "进程正在运行"
}

// 🔧 改进的锁定错误检查
func isLockError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// BadgerDB 的典型锁定错误信息
	lockKeywords := []string{
		"Cannot acquire directory lock",
		"resource temporarily unavailable",
		"另一个进程正在使用",
		"LOCK",
	}

	for _, keyword := range lockKeywords {
		if strings.Contains(errStr, keyword) {
			return true
		}
	}

	// 系统级锁定错误
	return os.IsExist(err) ||
		syscall.EAGAIN.Error() == errStr ||
		os.IsPermission(err)
}

// NewBadgerDBWithConfig 使用配置创建 BadgerDB
func NewBadgerDBWithConfig[T types.IModel](config BadgerDBConfig) (*BadgerDB[T], error) {
	// 验证配置
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("配置验证失败: %w", err)
	}

	// 🆕 尝试清理旧的锁文件（仅在开发/测试环境）
	if config.Mode == "fast" || config.Mode == "test" {
		lockFile := filepath.Join(config.Path, "LOCK")
		if _, err := os.Stat(lockFile); err == nil {
			// 🔧 先诊断锁定情况
			diagnosis := diagnoseLockError(config.Path)
			logx.Infof("发现锁文件: %s", diagnosis)

			// 尝试删除锁文件（可能失败，这是正常的）
			if err := os.Remove(lockFile); err != nil {
				logx.Errorf("无法删除锁文件: %v", err)
			} else {
				logx.Info("已清理旧锁文件")
			}
		}
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

	// 🆕 添加重试逻辑
	var db *badger.DB
	var err error
	maxRetries := 3

	for i := 0; i < maxRetries; i++ {
		db, err = badger.Open(opts)
		if err == nil {
			break
		}

		// 检查是否是锁定错误
		if isLockError(err) {
			// 🔧 详细诊断
			diagnosis := diagnoseLockError(config.Path)

			if i < maxRetries-1 {
				logx.Errorf("数据库被锁定，等待重试... (%d/%d)\n详情: %s", i+1, maxRetries, diagnosis)
				time.Sleep(time.Second * time.Duration(i+1))
				continue
			} else {
				// 🔧 最后一次重试失败，返回详细错误
				return nil, fmt.Errorf("打开 BadgerDB 失败（已重试 %d 次）: %s\n原始错误: %w",
					maxRetries, diagnosis, err)
			}
		}

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

	if config.AutoCleanup {
		b.SetAutoCleanup(true)
	}

	logx.Infof("BadgerDB 已启动 [mode=%s, path=%s, autoSync=%v, autoCleanup=%v]",
		config.Mode, config.Path, config.AutoSync, config.AutoCleanup)

	return b, nil
}

// 🆕 添加手动检查锁定状态的方法
func CheckDatabaseLock(dbPath string) error {
	lockFile := filepath.Join(dbPath, "LOCK")

	if _, err := os.Stat(lockFile); os.IsNotExist(err) {
		return nil // 无锁
	}

	diagnosis := diagnoseLockError(dbPath)
	return fmt.Errorf("数据库已被锁定: %s", diagnosis)
}

// 🆕 强制释放锁（危险操作，仅用于恢复）
func ForceUnlock(dbPath string) error {
	lockFile := filepath.Join(dbPath, "LOCK")

	// 先诊断
	diagnosis := diagnoseLockError(dbPath)
	logx.Errorf("强制解锁前诊断: %s", diagnosis)

	// 删除锁文件
	if err := os.Remove(lockFile); err != nil {
		return fmt.Errorf("删除锁文件失败: %w", err)
	}

	logx.Info("已强制删除锁文件")
	return nil
}
func (b *BadgerDB[T]) getDataAction(item *T) types.IDataAction {
	if b.syncList != nil {
		model := item
		if model == nil {
			model = new(T)
			if nm, ok := any(model).(types.IModelNewHook); ok {
				nm.NewModel()
			}
		}
		searchItem := b.syncList.GetSearchItem()
		searchItem.Model = model
		action := b.syncList.GetDBAdapter(searchItem)
		return action
	}
	return nil
}
func (b *BadgerDB[T]) SetAutoCleanup(auto bool) {
	b.isAutoClean = auto
	if auto {
		// 🔧 启动自动清理
		b.cleanupOnce.Do(func() {
			b.wg.Add(1)
			go b.autoCleanup()
			logx.Info("自动清理已启动")
		})
	}
}

// SetSyncDB 设置同步数据库
func (b *BadgerDB[T]) SetSyncDB(list *entity.ModelList[T]) {
	b.syncLock.Lock()
	defer b.syncLock.Unlock()
	if list != nil {
		if b.syncDB {
			return
		}
		b.syncDB = true
	} else {
		if !b.syncDB {
			return
		}
		b.syncDB = false
	}

	b.syncList = list

	if list != nil && b.syncDB {
		// 🔧 启动自动同步
		b.syncOnce.Do(func() {
			b.wg.Add(1)
			go b.syncToOtherDB()
			logx.Info("自动同步已启动")
		})
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

// 🆕 快速获取待同步数量（带缓存）
func (b *BadgerDB[T]) GetPendingSyncCountFast() (int, error) {
	b.pendingCountMutex.RLock()

	// 缓存有效期 1 秒
	if time.Since(b.lastCountUpdate) < time.Second {
		count := b.pendingCountCache
		b.pendingCountMutex.RUnlock()
		return count, nil
	}

	b.pendingCountMutex.RUnlock()

	// 重新计数
	count, err := b.GetPendingSyncCount()
	if err != nil {
		return 0, err
	}

	b.pendingCountMutex.Lock()
	b.pendingCountCache = count
	b.lastCountUpdate = time.Now()
	b.pendingCountMutex.Unlock()

	return count, nil
}

// 🆕 更新待同步计数（在写入/删除时调用）
func (b *BadgerDB[T]) incrementPendingCount(delta int) {
	b.pendingCountMutex.Lock()
	b.pendingCountCache += delta
	b.pendingCountMutex.Unlock()
}

// Set 写入数据
func (b *BadgerDB[T]) Set(item *T, ttl time.Duration, fn ...func(wrapper *SyncQueueItem[T])) error {
	key := b.generateKey(item)
	if key == "" {
		return badger.ErrEmptyKey
	}

	b.syncLock.RLock()
	needSync := b.syncDB
	b.syncLock.RUnlock()

	data, err := b.setItem(key, needSync, item, fn...)
	err = b.db.Update(func(txn *badger.Txn) error {
		entry := badger.NewEntry([]byte(key), data)
		if ttl > 0 {
			entry = entry.WithTTL(ttl)
		}
		return txn.SetEntry(entry)
	})

	// 🆕 更新待同步计数
	if err == nil && needSync {
		b.incrementPendingCount(1)
	}
	return err
}
func (b *BadgerDB[T]) setItem(key string, needSync bool, item *T, fn ...func(wrapper *SyncQueueItem[T])) ([]byte, error) {
	if item == nil {
		return nil, fmt.Errorf("item 不能为空")
	}
	existingWrapper, err := b.getWrapper(key)
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
func (b *BadgerDB[T]) batchInsert(items []*T, fn ...func(wrapper *SyncQueueItem[T])) error {
	if len(items) == 0 {
		return nil
	}

	b.syncLock.RLock()
	needSync := b.syncDB
	b.syncLock.RUnlock()

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
		value, err := b.setItem(key, needSync, item, fn...)
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
			// 🆕 更新待同步计数
			if needSync {
				b.incrementPendingCount(1)
			}
			return nil
		}

		txn.Discard()
		time.Sleep(time.Millisecond * 100 * time.Duration(retry+1))
	}

	return lastErr
}

// BatchInsert 批量插入
func (b *BadgerDB[T]) BatchInsert(items []*T) error {
	return b.batchInsert(items)
}

// BatchInsertWithBack 带回调的批量插入
func (b *BadgerDB[T]) BatchInsertWithBack(items []*T, fn ...func(wrapper *SyncQueueItem[T])) error {
	return b.batchInsert(items, fn...)
}
func (b *BadgerDB[T]) DeleteByItem(item *T) error {
	if item == nil {
		return fmt.Errorf("item 不能为空")
	}
	key := b.generateKey(item)
	if key == "" {
		return badger.ErrEmptyKey
	}
	b.syncLock.RLock()
	needSync := b.syncDB
	b.syncLock.RUnlock()
	return b.delete(key, needSync)
}
func (b *BadgerDB[T]) DeleteByItemWithSync(item *T, needSync bool) error {
	if item == nil {
		return fmt.Errorf("item 不能为空")
	}
	key := b.generateKey(item)
	if key == "" {
		return badger.ErrEmptyKey
	}
	return b.delete(key, needSync)
}

// Delete 删除数据（支持软删除）
func (b *BadgerDB[T]) Delete(key string) error {
	b.syncLock.RLock()
	needSync := b.syncDB
	b.syncLock.RUnlock()
	return b.delete(key, needSync)
}
func (b *BadgerDB[T]) delete(key string, needSync bool) error {
	if !needSync {
		// 🔧 不需要同步，直接物理删除
		return b.db.Update(func(txn *badger.Txn) error {
			return txn.Delete([]byte(key))
		})
	}
	if !b.syncDB {
		return fmt.Errorf("未启用同步数据库功能，无法执行软删除")
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
			// ✅ 修复：limit <= 0 表示不限制
			if limit > 0 && count >= limit {
				break
			}

			item := it.Item()

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

// ScanResult 分页扫描结果
type ScanResult[T types.IModel] struct {
	Items   []*T   `json:"items"`    // 数据列表
	LastKey string `json:"last_key"` // 最后一个 key（用于下次分页）
	HasMore bool   `json:"has_more"` // 是否还有更多数据
}

// ScanPage 分页扫描数据（基于游标）
// prefix: key 前缀
// limit: 每页数量
// lastKey: 上一页的最后一个 key（首次传空字符串）
func (b *BadgerDB[T]) ScanPage(prefix string, limit int, lastKey string) (*ScanResult[T], error) {
	if limit <= 0 {
		limit = 1000 // 默认每页 1000 条
	}

	result := &ScanResult[T]{
		Items: make([]*T, 0, limit),
	}

	err := b.db.View(func(txn *badger.Txn) error {
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

	_, err = b.syncBatch(unsyncedItems)
	if err != nil {
		logx.Errorf("批量同步失败: %v", err)
	}

	// if len(successKeys) > 0 {
	// 	if err := b.handleSyncedItems(successKeys); err != nil {
	// 		logx.Errorf("处理已同步数据失败: %v", err)
	// 	} else {
	// 		logx.Infof("成功同步 %d 条数据", len(successKeys))
	// 	}
	// }

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
func setHashCode(item interface{}) {
	if code, ok := item.(types.IRowCode); ok {
		code.SetHashcode(code.GetHash())
	}
}

// syncBatch 批量同步数据
func (b *BadgerDB[T]) syncBatch(items []*SyncQueueItem[T]) ([]string, error) {
	successKeys := make([]string, 0, len(items))

	b.syncLock.RLock()
	defer b.syncLock.RUnlock()

	if !b.syncDB {
		return nil, fmt.Errorf("未开启 syncDB ")
	}

	defer func() {
		if r := recover(); r != nil {
			logx.Errorf("同步 panic: %v", r)
		}
	}()

	for _, wrapper := range items {
		var err error
		syncAction := b.getDataAction(wrapper.Item)
		if syncAction == nil {
			logx.Errorf("未找到同步操作对象 [%s]", wrapper.Key)
			continue
		}
		setHashCode(wrapper.Item)
		switch wrapper.Op {
		case OpInsert:
			if wrapper.Item != nil {
				err = syncAction.Insert(wrapper.Item)
				logx.Infof("同步插入操作 [%s]", wrapper.Key)
				if err != nil {
					if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "UNIQUE constraint failed") {
						logx.Infof("数据已存在，尝试更新操作 [%s]", wrapper.Key)
						err = nil
					}
				}
				if err == nil {
					if err1 := b.updateSyncedItem(wrapper); err1 != nil {
						logx.Errorf("更新已同步数据失败 [%s]: %v", wrapper.Key, err1)
					}
				}
			}
		case OpUpdate:
			if wrapper.Item != nil {
				err = syncAction.Update(wrapper.Item)
				logx.Infof("同步更新操作 [%s]", wrapper.Key)
				if err == nil {
					if err1 := b.updateSyncedItem(wrapper); err1 != nil {
						logx.Errorf("更新已同步数据失败 [%s]: %v", wrapper.Key, err1)
					}
				}
			}
		case OpDelete:
			// 🔧 同步删除操作
			if wrapper.Item != nil {
				err = syncAction.Delete(wrapper.Item)
				logx.Infof("同步删除操作 [%s]", wrapper.Key)
				if err == nil {
					if err1 := b.delete(wrapper.Key, false); err1 != nil {
						logx.Errorf("物理删除失败 [%s]: %v", wrapper.Key, err1)
					}
				}
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
	return successKeys, nil
}
func (b *BadgerDB[T]) updateSyncedItem(wrapper *SyncQueueItem[T]) error {
	return b.db.Update(func(txn *badger.Txn) error {
		// 🔧 标记为已同步
		wrapper.IsSynced = true
		wrapper.SyncedAt = time.Now()
		wrapper.Op = OpUpdate

		data, err := json.Marshal(&wrapper)
		if err != nil {
			return err
		}

		if err := txn.Set([]byte(wrapper.Key), data); err != nil {
			return err
		}
		return nil
	})
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
	hasDB := b.syncList != nil
	b.syncLock.RUnlock()

	if !hasDB {
		return fmt.Errorf("未开启 syncDB ")
	}

	return b.processSyncQueue()
}

// CleanupAfterSync 清理已同步的数据
func (b *BadgerDB[T]) CleanupAfterSync(keepDuration time.Duration) error {
	count := 0
	cutoffTime := time.Now().Add(-keepDuration)

	// 第一步：收集需要删除的 key
	var keysToDelete []string

	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := string(item.Key())

			err := item.Value(func(val []byte) error {
				var wrapper SyncQueueItem[T]
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return err
				}

				count++

				// 清理已同步且超过保留时间的数据
				if wrapper.IsSynced && !wrapper.SyncedAt.IsZero() && wrapper.SyncedAt.Before(cutoffTime) {
					keysToDelete = append(keysToDelete, key)
				}

				return nil
			})

			if err != nil {
				logx.Errorf("读取数据失败: %v", err)
			}
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("收集待删除数据失败: %w", err)
	}

	if len(keysToDelete) == 0 {
		logx.Infof("清理完成: 检查 %d 条，无需删除", count)
		return nil
	}

	// 第二步：分批删除
	const batchSize = 1000
	deletedCount := 0

	for i := 0; i < len(keysToDelete); i += batchSize {
		end := i + batchSize
		if end > len(keysToDelete) {
			end = len(keysToDelete)
		}

		batch := keysToDelete[i:end]

		err := b.db.Update(func(txn *badger.Txn) error {
			for _, key := range batch {
				if err := txn.Delete([]byte(key)); err != nil {
					return err
				}
				deletedCount++
			}
			return nil
		})

		if err != nil {
			logx.Errorf("批量删除失败 [batch %d-%d]: %v", i, end, err)
			continue
		}
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

// syncToOtherDB 使用快速计数
func (b *BadgerDB[T]) syncToOtherDB() {
	defer b.wg.Done()

	interval := b.config.SyncInterval
	minInterval := b.config.SyncMinInterval
	maxInterval := b.config.SyncMaxInterval

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			b.syncLock.RLock()
			hasDB := b.syncDB
			b.syncLock.RUnlock()

			if !hasDB {
				continue
			}

			// 🔧 使用快速缓存计数
			pendingCount, err := b.GetPendingSyncCountFast()
			if err != nil {
				logx.Errorf("获取待同步数量失败: %v", err)
				continue
			}

			if pendingCount == 0 {
				interval = min(interval*2, maxInterval)
				ticker.Reset(interval)
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

			// 🆕 同步后重置缓存
			b.pendingCountMutex.Lock()
			b.pendingCountCache = 0
			b.lastCountUpdate = time.Time{}
			b.pendingCountMutex.Unlock()

			if duration < interval/2 {
				interval = max(interval/2, minInterval)
			} else if duration > interval {
				interval = min(duration*2, maxInterval)
			}

			ticker.Reset(interval)
			logx.Infof("同步完成 [处理: %d, 耗时: %v, 下次间隔: %v]",
				pendingCount, duration, interval)

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

// Count 获取数据库中的数据总数（不包括已删除的数据）
func (b *BadgerDB[T]) Count() (int, error) {
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
					return nil // 忽略解析错误
				}

				// 只统计未删除的数据
				if !wrapper.IsDeleted {
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

// CountAll 获取数据库中的所有数据总数（包括已删除的数据）
func (b *BadgerDB[T]) CountAll() (int, error) {
	count := 0

	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false // 不需要读取值
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			count++
		}
		return nil
	})

	return count, err
}

// CountByPrefix 统计指定前缀的数据数量（不包括已删除）
func (b *BadgerDB[T]) CountByPrefix(prefix string) (int, error) {
	count := 0

	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek([]byte(prefix)); it.ValidForPrefix([]byte(prefix)); it.Next() {
			item := it.Item()

			err := item.Value(func(val []byte) error {
				var wrapper SyncQueueItem[T]
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return nil
				}

				if !wrapper.IsDeleted {
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

// GetStatistics 获取数据库统计信息
func (b *BadgerDB[T]) GetStatistics() (*DBStatistics, error) {
	stats := &DBStatistics{}

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
					return nil
				}

				stats.TotalCount++

				if wrapper.IsDeleted {
					stats.DeletedCount++
				} else {
					stats.ActiveCount++
				}

				if !wrapper.IsSynced {
					stats.UnsyncedCount++
				} else {
					stats.SyncedCount++
				}

				return nil
			})

			if err != nil {
				continue
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	// 获取数据库大小
	lsm, vlog, _ := b.GetDBSize()
	stats.LSMSize = lsm
	stats.VLogSize = vlog
	stats.TotalSize = lsm + vlog

	return stats, nil
}

// DBStatistics 数据库统计信息
type DBStatistics struct {
	TotalCount    int   `json:"total_count"`    // 总数据量
	ActiveCount   int   `json:"active_count"`   // 活跃数据（未删除）
	DeletedCount  int   `json:"deleted_count"`  // 已删除数据
	SyncedCount   int   `json:"synced_count"`   // 已同步数据
	UnsyncedCount int   `json:"unsynced_count"` // 未同步数据
	LSMSize       int64 `json:"lsm_size"`       // LSM 大小（字节）
	VLogSize      int64 `json:"vlog_size"`      // VLog 大小（字节）
	TotalSize     int64 `json:"total_size"`     // 总大小（字节）
}

// String 格式化输出统计信息
func (s *DBStatistics) String() string {
	return fmt.Sprintf(
		"总数: %d, 活跃: %d, 已删除: %d, 已同步: %d, 未同步: %d, LSM: %dMB, VLog: %dMB, 总大小: %dMB",
		s.TotalCount,
		s.ActiveCount,
		s.DeletedCount,
		s.SyncedCount,
		s.UnsyncedCount,
		s.LSMSize/(1024*1024),
		s.VLogSize/(1024*1024),
		s.TotalSize/(1024*1024),
	)
}
