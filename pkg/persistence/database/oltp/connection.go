package oltp

import (
	"context"
	"fmt"
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

func ClearTableCache() {
	tableCache = sync.Map{}
	connManager.CloseAll()
}

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

func GetConnectionManager() *ConnectionManager {
	return connManager
}
func (cm *ConnectionManager) GetAllConnections() map[string]*ConnectionInfo {
	cm.mutex.RLock()
	defer cm.mutex.RUnlock()

	// 返回连接的副本以避免并发修改
	copy := make(map[string]*ConnectionInfo)
	for k, v := range cm.connections {
		copy[k] = v
	}
	return copy
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
		// 🔧 优化数据库连接池配置
		if sqlDB, err := db.DB(); err == nil {
			sqlDB.SetMaxOpenConns(10)                  // 最大打开连接数
			sqlDB.SetMaxIdleConns(5)                   // 最大空闲连接数
			sqlDB.SetConnMaxLifetime(30 * time.Minute) // 连接最大生命周期
			sqlDB.SetConnMaxIdleTime(10 * time.Minute) // 空闲连接最大存活时间
		}

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
		defer func() {
			if err := recover(); err != nil {
				logx.Errorf("全局清理goroutine panic: %v", err)
				// 重启清理任务
				time.Sleep(5 * time.Second)
				startGlobalCleanup()
			}
		}()

		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			// 🔧 为整个清理过程设置超时
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)

			cleanupTasks := []struct {
				name string
				fn   func(context.Context)
			}{
				{"清理过期连接", func(ctx context.Context) {
					select {
					case <-ctx.Done():
						return
					default:
						connManager.CleanupExpired()
					}
				}},
				{"清理表缓存", func(ctx context.Context) {
					select {
					case <-ctx.Done():
						return
					default:
						cleanExpiredTableCache()
					}
				}},
				{"检查连接健康", func(ctx context.Context) { checkConnectionHealthWithContext(ctx) }},
				{"智能GC", func(ctx context.Context) {
					select {
					case <-ctx.Done():
						return
					default:
						performSmartGC()
					}
				}},
				{"记录统计", func(ctx context.Context) {
					select {
					case <-ctx.Done():
						return
					default:
						logCleanupStats()
					}
				}},
			}

			// 🔧 串行执行清理任务，每个都有超时保护
			for _, task := range cleanupTasks {
				select {
				case <-ctx.Done():
					logx.Alert("清理任务超时，跳过后续任务")
					break
				default:
					func() {
						defer func() {
							if err := recover(); err != nil {
								logx.Errorf("清理任务 %s panic: %v", task.name, err)
							}
						}()

						taskCtx, taskCancel := context.WithTimeout(ctx, 20*time.Second)
						task.fn(taskCtx)
						taskCancel()
					}()
				}
			}

			cancel()
		}
	}()
}

// 🔧 新增：带上下文的连接健康检查
func checkConnectionHealthWithContext(ctx context.Context) {
	done := make(chan struct{}, 1)
	go func() {
		checkConnectionHealth()
		done <- struct{}{}
	}()

	select {
	case <-done:
		// 检查完成
	case <-ctx.Done():
		logx.Alert("连接健康检查被上下文取消")
	}
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

// 🔧 修复：带超时的连接健康检查
func checkConnectionHealth() {
	// 🔧 使用带超时的上下文
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	connManager.mutex.RLock()
	keys := make([]string, 0, len(connManager.connections))
	infos := make([]*ConnectionInfo, 0, len(connManager.connections))
	for key, info := range connManager.connections {
		keys = append(keys, key)
		infos = append(infos, info)
	}
	connManager.mutex.RUnlock()

	// 🔧 并发检查连接健康状态，但限制并发数
	semaphore := make(chan struct{}, 5) // 最多5个并发检查
	var wg sync.WaitGroup

	for i, key := range keys {
		if ctx.Err() != nil {
			break // 如果上下文已取消，停止检查
		}

		wg.Add(1)
		go func(k string, info *ConnectionInfo) {
			defer wg.Done()

			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()

				// 🔧 带超时的ping检查
				done := make(chan error, 1)
				go func() {
					if sqlDB, err := info.DB.DB(); err == nil {
						done <- sqlDB.PingContext(ctx)
					} else {
						done <- err
					}
				}()

				select {
				case err := <-done:
					if err != nil {
						logx.Errorf("数据库连接不健康，移除: %s, 错误: %v", k, err)
						if sqlDB, dbErr := info.DB.DB(); dbErr == nil {
							sqlDB.Close()
						}
						connManager.SetConnection(k, nil)
					}
				case <-ctx.Done():
					logx.Alert(fmt.Sprintf("数据库连接健康检查超时: %s", k))
				}

			case <-ctx.Done():
				return
			}
		}(key, infos[i])
	}

	// 🔧 等待所有检查完成，但有超时
	done := make(chan struct{})
	go func() {
		wg.Wait()
		done <- struct{}{}
	}()

	select {
	case <-done:
		// 所有检查完成
	case <-ctx.Done():
		logx.Alert("连接健康检查整体超时")
	}
}
func performSmartGC() {
	// 🔧 使用超时防止阻塞
	done := make(chan struct{}, 1)
	go func() {
		defer func() {
			if err := recover(); err != nil {
				logx.Errorf("智能GC执行时panic: %v", err)
			}
			done <- struct{}{}
		}()

		var current runtime.MemStats
		runtime.ReadMemStats(&current)

		goroutineCount := runtime.NumGoroutine()
		shouldGC := false

		if lastMemStats.Alloc > 0 {
			growthRate := float64(current.Alloc) / float64(lastMemStats.Alloc)
			if growthRate > 1.3 || current.Alloc > 150*1024*1024 {
				shouldGC = true
			}
		}

		if goroutineCount > 1000 {
			shouldGC = true
		}

		if shouldGC {
			logx.Infof("执行智能GC - 内存增长率: %.2f, 当前内存: %dMB, Goroutines: %d",
				float64(current.Alloc)/float64(lastMemStats.Alloc),
				current.Alloc/1024/1024,
				goroutineCount)
			runtime.GC()
		}

		lastMemStats = current
	}()

	// 🔧 如果GC执行超时，直接返回
	select {
	case <-done:
		// GC完成
	case <-time.After(10 * time.Second):
		logx.Alert("智能GC执行超时")
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
