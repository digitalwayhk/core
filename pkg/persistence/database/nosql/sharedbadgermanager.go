package nosql

import (
	"fmt"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v3"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/zeromicro/go-zero/core/logx"
)

// badgerLogger 日志适配器
type badgerLogger struct{}

func (l *badgerLogger) Errorf(f string, v ...interface{})   { logx.Errorf(f, v...) }
func (l *badgerLogger) Warningf(f string, v ...interface{}) { logx.Infof(f, v...) }
func (l *badgerLogger) Infof(f string, v ...interface{})    { logx.Infof(f, v...) }
func (l *badgerLogger) Debugf(f string, v ...interface{})   {}

type ISyncAfterDelete[T types.IModel] interface {
	IsSyncAfterDelete() bool
}
type IOnSyncAfter[T types.IModel] interface {
	OnSyncAfter(items []*SyncQueueItem[T]) error
}

// 🆕 IAutoCleanup 自动清理接口
type IAutoCleanup[T types.IModel] interface {
	IsAutoCleanup(item *SyncQueueItem[T]) bool
}

// 🆕 IAutoLimit 自动限制数量接口
type IAutoLimit[T types.IModel] interface {
	// GetLimitConfig 获取限制配置
	// 返回: (筛选前缀, 最大保留条数, 排序字段, 是否降序)
	// 例如: ("user:active:", 1000, "created_at", true) 表示只保留最新的1000条活跃用户
	GetLimitConfig() (filterPrefix string, maxCount int, sortField string, descending bool)
}

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
		SyncInterval:         2 * time.Second,
		SyncMinInterval:      1 * time.Second,
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
		cfg.Path = basePath
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
		WithLogger(nil).
		WithLoggingLevel(badger.WARNING).
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
			// 🆕 记录 GC 前的数据库大小
			lsmBefore, vlogBefore := m.db.Size()

			const maxGCRounds = 10
			var reclaimed int

			for i := 0; i < maxGCRounds; i++ {
				err := m.db.RunValueLogGC(m.config.GCDiscardRatio)
				if err != nil {
					if err == badger.ErrNoRewrite {
						break
					}
					logx.Errorf("共享DB GC失败 (round %d): %v", i+1, err)
					break
				}
				reclaimed++
			}

			// 🆕 记录 GC 后的数据库大小
			lsmAfter, vlogAfter := m.db.Size()

			if reclaimed > 0 {
				fmt.Printf("共享DB GC完成 [回收: %d轮, LSM: %dMB->%dMB, VLog: %dMB->%dMB]",
					reclaimed,
					lsmBefore/(1024*1024), lsmAfter/(1024*1024),
					vlogBefore/(1024*1024), vlogAfter/(1024*1024),
				)
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
