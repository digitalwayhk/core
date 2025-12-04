package sync

import (
	"fmt"
	"reflect"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/zeromicro/go-zero/core/logx"
)

// 智能同步助手
type SyncHelper struct {
	manager *DBSyncManager
}

func NewSyncHelper(manager *DBSyncManager) *SyncHelper {
	return &SyncHelper{manager: manager}
}

// 🔧 获取所有数据库连接信息
func (h *SyncHelper) GetAllDatabases() map[string]*oltp.ConnectionInfo {
	connManager := oltp.GetConnectionManager()
	return connManager.GetAllConnections()
}

// 🔧 获取数据库数量
func (h *SyncHelper) GetDatabaseCount() int {
	return len(h.GetAllDatabases())
}

// 智能检测需要同步的表
func (h *SyncHelper) DetectSyncTables(model interface{}) ([]string, error) {
	var tables []string

	// 使用反射获取模型的嵌套结构
	t := reflect.TypeOf(model)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	tables = append(tables, t.Name())

	// 递归检查嵌套表
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Type.Kind() == reflect.Slice {
			elemType := field.Type.Elem()
			if elemType.Kind() == reflect.Ptr {
				elemType = elemType.Elem()
			}
			if elemType.Kind() == reflect.Struct {
				tables = append(tables, elemType.Name())
			}
		}
	}

	return tables, nil
}

// 生成同步报告
func (h *SyncHelper) GenerateSyncReport() string {
	status, err := h.manager.GetStatus()
	stats := h.manager.GetStats()

	total, failed, toRemote, fromRemote, lastSync := stats.GetStats()

	// 🔧 处理零时间
	lastSyncStr := "从未同步"
	if !lastSync.IsZero() {
		lastSyncStr = lastSync.Format("2006-01-02 15:04:05")
	}

	errStr := "无"
	if err != nil {
		errStr = err.Error()
	}

	report := fmt.Sprintf(`
========================================
📊 数据库同步报告
========================================
状态: %s
数据库数量: %d
最后同步: %s
总同步次数: %d
失败次数: %d
上传次数: %d (SQLite -> MySQL)
下载次数: %d (MySQL -> SQLite)
最后错误: %s
========================================
`, statusToString(status), h.GetDatabaseCount(), lastSyncStr,
		total, failed, toRemote, fromRemote, errStr)

	return report
}

func statusToString(status SyncStatus) string {
	switch status {
	case SyncStatusIdle:
		return "空闲"
	case SyncStatusRunning:
		return "运行中"
	case SyncStatusPaused:
		return "已暂停"
	case SyncStatusError:
		return "错误"
	default:
		return "未知"
	}
}

// 健康检查
func (h *SyncHelper) HealthCheck() error {
	status, err := h.manager.GetStatus()

	if status == SyncStatusError {
		return fmt.Errorf("同步服务错误: %v", err)
	}

	stats := h.manager.GetStats()
	_, _, _, _, lastSync := stats.GetStats()

	// 🔧 修复：检查是否从未同步过
	if lastSync.IsZero() {
		// 如果服务正在运行，允许首次同步尚未完成
		if status == SyncStatusRunning {
			return nil // 首次启动，还未同步
		}
		return fmt.Errorf("同步服务从未执行过同步")
	}

	// 检查是否长时间未同步
	timeSinceLastSync := time.Since(lastSync)
	if timeSinceLastSync > 30*time.Minute {
		return fmt.Errorf("同步超时: 上次同步时间 %v (%v 前)",
			lastSync.Format("2006-01-02 15:04:05"), timeSinceLastSync.Round(time.Second))
	}

	return nil
}

// 强制全量同步
func (h *SyncHelper) ForceFullSync() error {
	logx.Info("🔄 开始强制全量同步...")
	h.manager.TriggerSync()
	return nil
}

// 智能冲突解决
func (h *SyncHelper) ResolveConflict(local, remote interface{}) (interface{}, error) {
	// 比较时间戳
	localTime := getTimestamp(local)
	remoteTime := getTimestamp(remote)

	if localTime.After(remoteTime) {
		return local, nil
	}
	return remote, nil
}

func getTimestamp(data interface{}) time.Time {
	v := reflect.ValueOf(data)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	// 尝试获取 UpdatedAt 字段
	if field := v.FieldByName("UpdatedAt"); field.IsValid() {
		if t, ok := field.Interface().(time.Time); ok {
			return t
		}
	}

	// 尝试获取 CreatedAt 字段
	if field := v.FieldByName("CreatedAt"); field.IsValid() {
		if t, ok := field.Interface().(time.Time); ok {
			return t
		}
	}

	return time.Time{}
}

// 🔧 新增：等待首次同步完成
func (h *SyncHelper) WaitForFirstSync(timeout time.Duration) error {
	startTime := time.Now()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			stats := h.manager.GetStats()
			_, _, _, _, lastSync := stats.GetStats()

			if !lastSync.IsZero() {
				return nil
			}

			if time.Since(startTime) > timeout {
				return fmt.Errorf("等待首次同步超时")
			}
		}
	}
}

// 🔧 新增：获取上次同步时间
func (h *SyncHelper) GetLastSyncTime() time.Time {
	stats := h.manager.GetStats()
	_, _, _, _, lastSync := stats.GetStats()
	return lastSync
}

// 🔧 新增：判断是否已同步过
func (h *SyncHelper) HasSynced() bool {
	return !h.GetLastSyncTime().IsZero()
}

// 🔧 新增：生成数据库连接报告
func (h *SyncHelper) GenerateDBConnectionReport() string {
	connections := h.GetAllDatabases()

	report := "\n========================================\n"
	report += "🗄️  数据库连接报告\n"
	report += "========================================\n"
	report += fmt.Sprintf("总连接数: %d\n", len(connections))
	report += "----------------------------------------\n"

	if len(connections) == 0 {
		report += "暂无数据库连接\n"
	} else {
		for dbKey, info := range connections {
			report += fmt.Sprintf("📦 %s\n", dbKey)
			report += fmt.Sprintf("   创建时间: %s\n", info.CreatedAt.Format("2006-01-02 15:04:05"))
			report += fmt.Sprintf("   最后使用: %s\n", info.LastUsed.Format("2006-01-02 15:04:05"))
			report += fmt.Sprintf("   使用时长: %v\n", time.Since(info.LastUsed).Round(time.Second))
			report += "----------------------------------------\n"
		}
	}

	report += "========================================\n"
	return report
}
