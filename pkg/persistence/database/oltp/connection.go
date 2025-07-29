package oltp

import (
	"sync"

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
