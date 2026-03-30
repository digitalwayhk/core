package nosql

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/digitalwayhk/core/pkg/json"

	"github.com/dgraph-io/badger/v3"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/zeromicro/go-zero/core/logx"
)

// BadgerDB 泛型 KV 数据库
type BadgerDB[T any] struct {
	db      *badger.DB
	path    string
	config  BadgerDBConfig // 🆕 配置
	closeCh chan struct{}
	wg      sync.WaitGroup

	// ✅ 添加关闭状态控制
	closeOnce sync.Once
	closed    bool
	mu        sync.RWMutex // 保护 closed 字段
}

// NewBadgerDB 创建生产环境 BadgerDB（保持向后兼容）
func NewBadgerDB[T any](path string) (*BadgerDB[T], error) {
	config := DefaultProductionConfig(path)
	return NewBadgerDBWithConfig[T](config)
}

// NewBadgerDBFast 创建快速模式 BadgerDB（保持向后兼容）
func NewBadgerDBFast[T any](path string) (*BadgerDB[T], error) {
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
	// Linux: 读取 /proc 目录
	cmdPath := fmt.Sprintf("/proc/%d/cmdline", pid)
	if content, err := os.ReadFile(cmdPath); err == nil {
		cmd := strings.ReplaceAll(string(content), "\x00", " ")
		return fmt.Sprintf("命令: %s", cmd)
	}

	// macOS/Unix: 使用 ps 命令（备用方案）
	// 注意：为避免引入 os/exec 依赖，这里简化处理
	// 实际生产环境可以使用 exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=")

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
func NewBadgerDBWithConfig[T any](config BadgerDBConfig) (*BadgerDB[T], error) {
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

		// 检查是否是 MANIFEST/文件缺失导致的损坏（进程中途 panic 后重启时出现）
		// BadgerDB 是本地缓存，数据源在 MySQL，清空重建是安全的
		if isCorruptionError(err) && i == 0 {
			logx.Errorf("BadgerDB 文件损坏，清空目录重建 [path=%s]: %v", config.Path, err)
			if clearErr := clearBadgerData(config.Path); clearErr != nil {
				logx.Errorf("清空BadgerDB目录失败: %v", clearErr)
				return nil, fmt.Errorf("打开 BadgerDB 失败: %w", err)
			}
			logx.Infof("BadgerDB 目录已清空，重新初始化 [path=%s]", config.Path)
			continue
		}

		return nil, fmt.Errorf("打开 BadgerDB 失败: %w", err)
	}

	b := &BadgerDB[T]{
		db:      db,
		path:    config.Path,
		config:  config,
		closeCh: make(chan struct{}),
	}

	// 启动 GC
	b.wg.Add(1)
	go b.runGC()

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
func (b *BadgerDB[T]) Set(key string, item *T, ttl time.Duration, fn ...func(oldItem *T)) error {
	if err := b.checkClosed(); err != nil {
		return err
	}
	if key == "" {
		key = b.generateKey(item) // ✅ 修复：使用 = 而不是 :=
		if key == "" {
			return badger.ErrEmptyKey
		}
	}
	data, err := b.setItem(key, item, fn...)
	if err != nil {
		return err
	}
	err = b.db.Update(func(txn *badger.Txn) error {
		entry := badger.NewEntry([]byte(key), data)
		if ttl > 0 {
			entry = entry.WithTTL(ttl)
		}
		return txn.SetEntry(entry)
	})
	return err
}
func (b *BadgerDB[T]) setItem(key string, item *T, fn ...func(old *T)) ([]byte, error) {
	if item == nil {
		return nil, fmt.Errorf("item 不能为空")
	}
	existingWrapper, err := b.getWrapper(key)
	// ✅ 修复：忽略 key 不存在的错误是正常的
	if err != nil && err != badger.ErrKeyNotFound {
		return nil, fmt.Errorf("获取现有数据失败: %w", err)
	}

	if len(fn) > 0 && existingWrapper != nil {
		fn[0](existingWrapper)
	}

	data, err := json.Marshal(item)
	if err != nil {
		return nil, fmt.Errorf("序列化失败: %w", err)
	}
	return data, nil
}
func (b *BadgerDB[T]) batchInsert(items []*T, fn ...func(wrapper *T)) error {
	if len(items) == 0 {
		return nil
	}

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
		value, err := b.setItem(key, item, fn...)
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

// BatchInsert 批量插入
func (b *BadgerDB[T]) BatchInsert(items []*T) error {
	if err := b.checkClosed(); err != nil {
		return err
	}
	return b.batchInsert(items)
}

// BatchInsertWithBack 带回调的批量插入
func (b *BadgerDB[T]) BatchInsertWithBack(items []*T, fn ...func(wrapper *T)) error {
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
	return b.delete(key)
}
func (b *BadgerDB[T]) DeleteByItemWithSync(item *T, needSync bool) error {
	if item == nil {
		return fmt.Errorf("item 不能为空")
	}
	key := b.generateKey(item)
	if key == "" {
		return badger.ErrEmptyKey
	}
	// ✅ 修复：使用 needSync 参数
	if err := b.delete(key); err != nil {
		return err
	}
	// 如果需要同步，立即刷新到磁盘
	if needSync {
		return b.db.Sync()
	}
	return nil
}

// Delete 删除数据（支持软删除）
func (b *BadgerDB[T]) Delete(key string) error {
	if err := b.checkClosed(); err != nil {
		return err
	}
	return b.delete(key)
}
func (b *BadgerDB[T]) delete(key string) error {
	// 🔧 不需要同步，直接物理删除
	return b.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(key))
	})
}

// Get 获取数据（过滤已删除的数据）
func (b *BadgerDB[T]) Get(key string) (*T, error) {
	if err := b.checkClosed(); err != nil {
		return nil, err
	}
	wrapper, err := b.getWrapper(key)
	if err != nil {
		return nil, err
	}

	return wrapper, nil
}

// getWrapper 获取包装对象（内部使用）
func (b *BadgerDB[T]) getWrapper(key string) (*T, error) {
	var wrapper = new(T)

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

	return wrapper, nil
}
func (b *BadgerDB[T]) ScanWithFn(prefix string, fn func(item *T) bool) ([]*T, error) {
	var results []*T
	err := b.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek([]byte(prefix)); it.ValidForPrefix([]byte(prefix)); it.Next() {
			item := it.Item()

			err := item.Value(func(val []byte) error {
				var wrapper T
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return err
				}
				if fn != nil {
					if fn(&wrapper) {
						results = append(results, &wrapper)
					}
				} else {
					results = append(results, &wrapper)
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
			// ✅ 修复：limit <= 0 表示不限制
			if limit > 0 && count >= limit {
				break
			}
			item := it.Item()

			err := item.Value(func(val []byte) error {
				var wrapper T
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return err
				}
				results = append(results, &wrapper)
				count++
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
				var wrapper T
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return err
				}

				results = append(results, &wrapper)
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
			// ✅ 修复：限制 GC 循环次数并检查关闭信号
			maxGCRuns := 10 // 每次最多运行 10 轮 GC
			for i := 0; i < maxGCRuns; i++ {
				// 检查是否需要退出
				select {
				case <-b.closeCh:
					logx.Info("runGC 在 GC 循环中收到退出信号")
					return
				default:
				}

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
	return b.CloseWithTimeout(10*time.Second, 5*time.Second)
}

// CloseWithTimeout 带超时的关闭数据库（推荐用于生产环境）
func (b *BadgerDB[T]) CloseWithTimeout(waitTimeout, syncTimeout time.Duration) error {
	var err error

	// ✅ 使用 sync.Once 确保只关闭一次
	b.closeOnce.Do(func() {
		// 标记为已关闭
		b.mu.Lock()
		b.closed = true
		b.mu.Unlock()

		// 关闭 channel，通知所有 goroutine 退出
		close(b.closeCh)

		// 等待 goroutine 退出（带超时）
		done := make(chan struct{})
		go func() {
			b.wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			logx.Info("后台 goroutine 已全部退出")
		case <-time.After(waitTimeout):
			logx.Errorf("等待后台 goroutine 退出超时（%v），强制关闭", waitTimeout)
		}

		// Sync 操作（带超时）
		syncDone := make(chan error, 1)
		go func() {
			syncDone <- b.db.Sync()
		}()

		select {
		case syncErr := <-syncDone:
			if syncErr != nil {
				logx.Errorf("关闭前 sync 失败: %v", syncErr)
			}
		case <-time.After(syncTimeout):
			logx.Errorf("sync 操作超时（%v），继续关闭", syncTimeout)
		}

		// 关闭数据库
		if closeErr := b.db.Close(); closeErr != nil {
			err = fmt.Errorf("关闭 BadgerDB 失败: %w", closeErr)
			return
		}

		logx.Info("BadgerDB 已关闭")
	})

	// 如果已经关闭过，返回提示
	b.mu.RLock()
	alreadyClosed := b.closed
	b.mu.RUnlock()

	if alreadyClosed && err == nil {
		// 不是第一次调用，但之前成功关闭了
		return nil
	}

	return err
}

// IsClosed 检查数据库是否已关闭
func (b *BadgerDB[T]) IsClosed() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.closed
}

// checkClosed 检查数据库是否已关闭，如果已关闭返回错误
func (b *BadgerDB[T]) checkClosed() error {
	if b.IsClosed() {
		return fmt.Errorf("数据库已关闭")
	}
	return nil
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

// GetConfig 获取当前配置
func (b *BadgerDB[T]) GetConfig() BadgerDBConfig {
	return b.config
}

// UpdateConfig 更新配置（部分参数）
func (b *BadgerDB[T]) UpdateConfig(updateFn func(*BadgerDBConfig)) error {

	oldConfig := b.config
	updateFn(&b.config)

	if err := b.config.Validate(); err != nil {
		b.config = oldConfig
		return fmt.Errorf("配置更新失败: %w", err)
	}

	logx.Infof("配置已更新: %+v", b.config)
	return nil
}

// CountAll 获取数据库中的所有数据总数（包括已删除的数据）
func (b *BadgerDB[T]) Count() (int, error) {
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
				var wrapper T
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return nil
				}
				count++

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
				var wrapper T
				if err := json.Unmarshal(val, &wrapper); err != nil {
					return nil
				}

				stats.TotalCount++
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
	TotalCount int   `json:"total_count"` // 总数据量
	LSMSize    int64 `json:"lsm_size"`    // LSM 大小（字节）
	VLogSize   int64 `json:"vlog_size"`   // VLog 大小（字节）
	TotalSize  int64 `json:"total_size"`  // 总大小（字节）
}

// String 格式化输出统计信息
func (s *DBStatistics) String() string {
	return fmt.Sprintf(
		"总数: %d, LSM: %dMB, VLog: %dMB, 总大小: %dMB",
		s.TotalCount,
		s.LSMSize/(1024*1024),
		s.VLogSize/(1024*1024),
		s.TotalSize/(1024*1024),
	)
}

// isCorruptionError 检测 BadgerDB 是否因进程中途崩溃导致的文件缺失/MANIFEST 损坏。
// 此类错误在服务 panic 后重启时出现：MANIFEST 已引用某文件但物理文件未创建。
// BadgerDB 在这种情况下是本地缓存，清空重建安全，数据源头在 MySQL。
func isCorruptionError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "no such file or directory") ||
		strings.Contains(s, "Value log truncate required") ||
		strings.Contains(s, "manifest file") ||
		strings.Contains(s, "MANIFEST")
}

// clearBadgerData 删除并重建 BadgerDB 目录（数据会丢失，只在确认是缓存场景时调用）。
func clearBadgerData(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("清空BadgerDB目录失败 [path=%s]: %w", path, err)
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("重建BadgerDB目录失败 [path=%s]: %w", path, err)
	}
	return nil
}
