package oltp

import (
	"runtime"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
)

var (
	// 全局表缓存，避免重复DDL解析
	tableCache    = sync.Map{}
	migrationLock = sync.Mutex{}
	connManager   = &ConnectionManager{
		connections: make(map[string]*ConnectionInfo),
	}
)

type TableCacheKey struct {
	DBPath    string
	TableName string
}

// 添加连接管理器
type ConnectionManager struct {
	mutex       sync.RWMutex
	connections map[string]*ConnectionInfo
}
type ConnectionInfo struct {
	DB        *gorm.DB
	CreatedAt time.Time
	LastUsed  time.Time
}

func (cm *ConnectionManager) GetConnection(key string) (*gorm.DB, bool) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	if info, exists := cm.connections[key]; exists {
		// 🔧 更新最后使用时间
		info.LastUsed = time.Now()
		return info.DB, true
	}
	return nil, false
}

func (cm *ConnectionManager) SetConnection(key string, db *gorm.DB) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	// 🔧 如果已存在，先关闭旧连接
	if oldInfo, exists := cm.connections[key]; exists && oldInfo.DB != nil {
		if sqlDB, err := oldInfo.DB.DB(); err == nil {
			sqlDB.Close()
		}
	}

	if db != nil {
		cm.connections[key] = &ConnectionInfo{
			DB:        db,
			CreatedAt: time.Now(),
			LastUsed:  time.Now(),
		}
	} else {
		delete(cm.connections, key)
	}
}

func (cm *ConnectionManager) CloseAll() {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	for key, info := range cm.connections {
		if sqlDB, err := info.DB.DB(); err == nil {
			sqlDB.Close()
		}
		delete(cm.connections, key)
	}
}

// 🔧 新增：清理过期连接
func (cm *ConnectionManager) CleanupExpired() {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	now := time.Now()
	for key, info := range cm.connections {
		// 清理超过10分钟未使用的连接
		if now.Sub(info.LastUsed) > 10*time.Minute {
			if sqlDB, err := info.DB.DB(); err == nil {
				sqlDB.Close()
			}
			delete(cm.connections, key)
			logx.Infof("清理过期数据库连接: %s", key)
		}
	}
}

// 在包级别启动一个全局清理goroutine
func init() {
	startGlobalCleanup()
}

// connection.go - 改进清理策略
func startGlobalCleanup() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute) // 🔧 更频繁的清理
		defer ticker.Stop()

		for range ticker.C {
			// 🔧 1. 清理过期连接
			connManager.CleanupExpired()

			// 🔧 2. 清理过期表缓存
			cleanExpiredTableCache()

			// 🔧 3. 检查连接健康状态
			checkConnectionHealth()

			// 🔧 4. 智能GC
			performSmartGC()

			// 🔧 5. 记录统计
			logCleanupStats()
		}
	}()
}

var lastMemStats runtime.MemStats

// 🔧 新增：清理过期表缓存
func cleanExpiredTableCache() {
	count := 0
	tableCache.Range(func(key, value interface{}) bool {
		count++
		return true
	})

	// 如果表缓存过多，清理一部分
	if count > 1000 {
		cleaned := 0
		tableCache.Range(func(key, value interface{}) bool {
			if cleaned < count/2 { // 清理一半
				tableCache.Delete(key)
				cleaned++
			}
			return cleaned < count/2
		})
		logx.Infof("清理表缓存: %d 个", cleaned)
	}
}

// 🔧 新增：检查连接健康状态
func checkConnectionHealth() {
	connManager.mutex.RLock()
	keys := make([]string, 0, len(connManager.connections))
	for key := range connManager.connections {
		keys = append(keys, key)
	}
	connManager.mutex.RUnlock()

	for _, key := range keys {
		if db, ok := connManager.GetConnection(key); ok {
			if sqlDB, err := db.DB(); err == nil {
				if err := sqlDB.Ping(); err != nil {
					logx.Errorf("数据库连接不健康，移除: %s, 错误: %v", key, err)
					sqlDB.Close()
					connManager.SetConnection(key, nil)
				}
			}
		}
	}
}
func performSmartGC() {
	var current runtime.MemStats
	runtime.ReadMemStats(&current)

	// 检查goroutine数量
	goroutineCount := runtime.NumGoroutine()

	// 🔧 基于多个指标决定是否GC
	shouldGC := false

	if lastMemStats.Alloc > 0 {
		growthRate := float64(current.Alloc) / float64(lastMemStats.Alloc)
		if growthRate > 1.3 || current.Alloc > 150*1024*1024 {
			shouldGC = true
		}
	}

	// 🔧 如果goroutine过多也执行GC
	if goroutineCount > 1000 {
		shouldGC = true
	}

	if shouldGC {
		logx.Infof("执行智能GC - 内存增长率: %.2f, 当前内存: %dMB, Goroutines: %d",
			float64(current.Alloc)/float64(lastMemStats.Alloc),
			current.Alloc/1024/1024,
			goroutineCount)
		runtime.GC()
		time.Sleep(100 * time.Millisecond) // 给GC一些时间
	}

	lastMemStats = current
}
func cleanConnections() {
	connManager.mutex.Lock()
	defer connManager.mutex.Unlock()

	for _, info := range connManager.connections {
		if sqlDB, err := info.DB.DB(); err == nil {
			// 重置连接池
			sqlDB.SetMaxIdleConns(0)
			time.Sleep(10 * time.Millisecond)
			sqlDB.SetMaxIdleConns(1)
		}
	}
}

func logCleanupStats() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	tableCount := 0
	tableCache.Range(func(key, value interface{}) bool {
		tableCount++
		return true
	})

	connManager.mutex.RLock()
	connCount := len(connManager.connections)
	connManager.mutex.RUnlock()

	goroutineCount := runtime.NumGoroutine()

	logx.Infof("📊 系统统计 - 内存: %dMB, Goroutines: %d, 表缓存: %d, DB连接: %d",
		m.Alloc/1024/1024,
		goroutineCount,
		tableCount,
		connCount)

	// 🔧 如果指标异常，发出警告
	if goroutineCount > 10000 {
		logx.Errorf("⚠️  Goroutine数量过多: %d", goroutineCount)
	}
	if m.Alloc/1024/1024 > 500 {
		logx.Errorf("⚠️  内存使用过高: %dMB", m.Alloc/1024/1024)
	}
	if connCount > 50 {
		logx.Errorf("⚠️  数据库连接过多: %d", connCount)
	}
}
