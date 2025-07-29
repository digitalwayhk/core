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
		connections: make(map[string]*gorm.DB),
	}
)

type TableCacheKey struct {
	DBPath    string
	TableName string
}

// 添加连接管理器
type ConnectionManager struct {
	connections map[string]*gorm.DB
	mutex       sync.RWMutex
}

func (cm *ConnectionManager) GetConnection(key string) (*gorm.DB, bool) {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	conn, exists := cm.connections[key]
	return conn, exists
}

func (cm *ConnectionManager) SetConnection(key string, db *gorm.DB) {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	cm.connections[key] = db
}

func (cm *ConnectionManager) CloseAll() {
	cm.mutex.Lock()
	defer cm.mutex.Unlock()

	for key, db := range cm.connections {
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.Close()
		}
		delete(cm.connections, key)
	}
}

// 在包级别启动一个全局清理goroutine
func init() {
	startGlobalCleanup()
}

func startGlobalCleanup() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			// 清理所有连接的缓存
			cleanConnections()

			// 执行垃圾回收
			runtime.GC()
			runtime.GC()

			// 记录清理统计
			logCleanupStats()
		}
	}()
}

func cleanConnections() {
	connManager.mutex.Lock()
	defer connManager.mutex.Unlock()

	for _, db := range connManager.connections {
		if sqlDB, err := db.DB(); err == nil {
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

	logx.Infof("清理统计 - 内存: %dMB, Goroutines: %d, 表缓存: %d, 连接数: %d",
		m.Alloc/1024/1024,
		runtime.NumGoroutine(),
		tableCount,
		len(connManager.connections))
}
