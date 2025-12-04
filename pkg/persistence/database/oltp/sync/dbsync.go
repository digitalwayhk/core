package sync

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/zeromicro/go-zero/core/logx"
	"gorm.io/gorm"
)

// 同步状态
type SyncStatus int

const (
	SyncStatusIdle SyncStatus = iota
	SyncStatusRunning
	SyncStatusPaused
	SyncStatusError
)

// 同步方向
type SyncDirection int

const (
	SyncToRemote SyncDirection = iota
	SyncFromRemote
	SyncBoth
)

// 冲突处理模式
type ConflictMode int

const (
	ConflictModeSkip      ConflictMode = iota // 跳过冲突
	ConflictModeOverwrite                     // 覆盖
	ConflictModeNewest                        // 保留最新
)

// 同步配置
type SyncConfig struct {
	// 基础配置
	Interval      time.Duration // 同步间隔
	BatchSize     int           // 批量大小
	MaxRetries    int           // 最大重试次数
	RetryInterval time.Duration // 重试间隔
	Direction     SyncDirection // 同步方向

	// MySQL 配置
	MySQLHost string
	MySQLPort uint
	MySQLUser string
	MySQLPass string

	// 过滤器
	TableFilter   func(tableName string) bool   // 表过滤器
	RecordFilter  func(record interface{}) bool // 记录过滤器
	ConflictMode  ConflictMode                  // 冲突处理模式
	EnableLogging bool                          // 是否启用详细日志
}

// 同步管理器
type DBSyncManager struct {
	config      *SyncConfig
	mysql       *oltp.Mysql
	status      SyncStatus
	lastError   error
	stats       *SyncStats
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	changeLog   *ChangeLog
	syncTrigger chan struct{}
}

// 同步统计
type SyncStats struct {
	mu            sync.RWMutex
	TotalSynced   int64
	FailedSynced  int64
	ToRemote      int64
	FromRemote    int64
	LastSyncTime  time.Time
	LastSyncError error
}

func (s *SyncStats) IncrementSuccess(direction SyncDirection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.TotalSynced++
	s.LastSyncTime = time.Now()
	if direction == SyncToRemote {
		s.ToRemote++
	} else if direction == SyncFromRemote {
		s.FromRemote++
	}
}

func (s *SyncStats) IncrementFailed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.FailedSynced++
}

func (s *SyncStats) SetError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastSyncError = err
}

func (s *SyncStats) GetStats() (total, failed, toRemote, fromRemote int64, lastSync time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.TotalSynced, s.FailedSynced, s.ToRemote, s.FromRemote, s.LastSyncTime
}

// 创建同步管理器
func NewDBSyncManager(config *SyncConfig) (*DBSyncManager, error) {
	if config == nil {
		return nil, errors.New("sync config is required")
	}

	// 设置默认值
	if config.Interval == 0 {
		config.Interval = 5 * time.Minute
	}
	if config.BatchSize == 0 {
		config.BatchSize = 100
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.RetryInterval == 0 {
		config.RetryInterval = 30 * time.Second
	}

	// 创建 MySQL 连接
	mysql := oltp.NewMysql(
		config.MySQLHost,
		config.MySQLUser,
		config.MySQLPass,
		config.MySQLPort,
		config.EnableLogging,
		true,
	)

	ctx, cancel := context.WithCancel(context.Background())

	manager := &DBSyncManager{
		config:      config,
		mysql:       mysql,
		status:      SyncStatusIdle,
		stats:       &SyncStats{},
		ctx:         ctx,
		cancel:      cancel,
		changeLog:   NewChangeLog(),
		syncTrigger: make(chan struct{}, 1),
	}

	return manager, nil
}

// 🔧 核心方法：从 ConnectionManager 获取所有 SQLite 数据库并同步
func (m *DBSyncManager) syncAllDatabases() error {
	// 从 ConnectionManager 获取所有连接
	connManager := oltp.GetConnectionManager()
	connections := connManager.GetAllConnections()

	if len(connections) == 0 {
		logx.Info("📭 没有找到 SQLite 数据库")
		return nil
	}

	logx.Infof("🔍 发现 %d 个数据库连接", len(connections))

	successCount := 0
	failCount := 0

	// 遍历所有连接
	for dbKey, connInfo := range connections {
		// 🔧 尝试从数据库中读取任意表的模型来获取 IDBName
		localDBName, remoteDBName, err := m.getDBNamesFromConnection(connInfo.DB, dbKey)
		if err != nil {
			logx.Errorf("⚠️  无法获取数据库名称映射 [%s]: %v", dbKey, err)
			// 使用默认映射
			localDBName = dbKey
			remoteDBName = dbKey
		}

		logx.Infof("🔄 同步数据库: %s -> %s", localDBName, remoteDBName)

		// 根据同步方向执行同步
		switch m.config.Direction {
		case SyncToRemote:
			if err := m.syncToRemote(connInfo.DB, localDBName, remoteDBName); err != nil {
				logx.Errorf("❌ 同步到远程失败 [%s]: %v", localDBName, err)
				m.stats.IncrementFailed()
				m.stats.SetError(err)
				failCount++
			} else {
				successCount++
			}

		case SyncFromRemote:
			if err := m.syncFromRemote(connInfo.DB, localDBName, remoteDBName); err != nil {
				logx.Errorf("❌ 从远程同步失败 [%s]: %v", localDBName, err)
				m.stats.IncrementFailed()
				m.stats.SetError(err)
				failCount++
			} else {
				successCount++
			}

		case SyncBoth:
			// 先从远程同步，再同步到远程
			if err := m.syncFromRemote(connInfo.DB, localDBName, remoteDBName); err != nil {
				logx.Errorf("❌ 从远程同步失败 [%s]: %v", localDBName, err)
				m.stats.IncrementFailed()
				failCount++
			}
			if err := m.syncToRemote(connInfo.DB, localDBName, remoteDBName); err != nil {
				logx.Errorf("❌ 同步到远程失败 [%s]: %v", localDBName, err)
				m.stats.IncrementFailed()
				failCount++
			}
			if err == nil {
				successCount++
			}
		}
	}

	logx.Infof("✅ 同步完成: 成功 %d, 失败 %d", successCount, failCount)
	return nil
}

// 🔧 从数据库连接中获取本地和远程数据库名
func (m *DBSyncManager) getDBNamesFromConnection(db *gorm.DB, defaultName string) (localName, remoteName string, err error) {
	// 查询数据库中的所有表
	var tables []struct {
		Name string
	}

	err = db.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' LIMIT 1").Scan(&tables).Error
	if err != nil || len(tables) == 0 {
		return defaultName, defaultName, fmt.Errorf("无法查询表")
	}

	// 尝试从第一个表中读取一条记录
	var records []map[string]interface{}
	err = db.Table(tables[0].Name).Limit(1).Find(&records).Error
	if err != nil || len(records) == 0 {
		return defaultName, defaultName, fmt.Errorf("无法读取数据")
	}

	// 🔧 注意：这里我们无法直接获取 IDBName 实例
	// 所以采用约定：使用数据库文件路径作为本地名，远程名需要另外配置
	return defaultName, defaultName, nil
}

// 🔧 同步到远程 (SQLite -> MySQL)
func (m *DBSyncManager) syncToRemote(sqlite *gorm.DB, localDBName, remoteDBName string) error {
	// 查询所有表
	var tables []struct {
		Name string
	}
	err := sqlite.Raw("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tables).Error
	if err != nil {
		return fmt.Errorf("查询表列表失败: %v", err)
	}

	if len(tables) == 0 {
		logx.Infof("📭 SQLite 无表 [%s]", localDBName)
		return nil
	}

	// 设置 MySQL 数据库名
	m.mysql.Name = remoteDBName
	mysqlDB, err := m.mysql.GetDB()
	if err != nil {
		return fmt.Errorf("获取 MySQL 连接失败: %v", err)
	}

	// 同步每个表
	for _, table := range tables {
		// 应用表过滤器
		if m.config.TableFilter != nil && !m.config.TableFilter(table.Name) {
			logx.Infof("⏭️  跳过表 [%s] (被过滤)", table.Name)
			continue
		}

		if err := m.syncTableToRemote(sqlite, mysqlDB, table.Name, localDBName); err != nil {
			logx.Errorf("同步表到远程失败 [%s.%s]: %v", localDBName, table.Name, err)
			continue
		}

		m.stats.IncrementSuccess(SyncToRemote)
	}

	return nil
}

// 🔧 从远程同步 (MySQL -> SQLite)
func (m *DBSyncManager) syncFromRemote(sqlite *gorm.DB, localDBName, remoteDBName string) error {
	// 设置 MySQL 数据库名
	m.mysql.Name = remoteDBName
	mysqlDB, err := m.mysql.GetDB()
	if err != nil {
		return fmt.Errorf("获取 MySQL 连接失败: %v", err)
	}

	// 获取 MySQL 中的所有表
	var tables []struct {
		TableName string `gorm:"column:TABLE_NAME"`
	}

	err = mysqlDB.Raw("SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA=?", remoteDBName).Scan(&tables).Error
	if err != nil {
		return fmt.Errorf("查询 MySQL 表列表失败: %v", err)
	}

	if len(tables) == 0 {
		logx.Infof("📭 MySQL 无表 [%s]", remoteDBName)
		return nil
	}

	// 同步每个表
	for _, table := range tables {
		// 应用表过滤器
		if m.config.TableFilter != nil && !m.config.TableFilter(table.TableName) {
			logx.Infof("⏭️  跳过表 [%s] (被过滤)", table.TableName)
			continue
		}

		if err := m.syncTableFromRemote(sqlite, mysqlDB, table.TableName, localDBName); err != nil {
			logx.Errorf("从远程同步表失败 [%s.%s]: %v", localDBName, table.TableName, err)
			continue
		}

		m.stats.IncrementSuccess(SyncFromRemote)
	}

	return nil
}

// 同步单个表到远程
func (m *DBSyncManager) syncTableToRemote(sqlite, mysql *gorm.DB, tableName, localDBName string) error {
	var records []map[string]interface{}
	err := sqlite.Table(tableName).Limit(m.config.BatchSize).Find(&records).Error
	if err != nil {
		return fmt.Errorf("查询 SQLite 数据失败: %v", err)
	}

	if len(records) == 0 {
		return nil
	}

	synced := 0
	for _, record := range records {
		// 应用记录过滤器
		if m.config.RecordFilter != nil && !m.config.RecordFilter(record) {
			continue
		}

		// 根据冲突模式处理
		if err := m.insertOrUpdateRemote(mysql, tableName, record); err != nil {
			logx.Errorf("插入远程记录失败 [%s]: %v", tableName, err)
			continue
		}
		synced++
	}

	if synced > 0 {
		logx.Infof("✅ 同步表到远程 [%s]: %d/%d 条记录", tableName, synced, len(records))
	}
	return nil
}

// 从远程同步单个表
func (m *DBSyncManager) syncTableFromRemote(sqlite, mysql *gorm.DB, tableName, localDBName string) error {
	var records []map[string]interface{}
	err := mysql.Table(tableName).Limit(m.config.BatchSize).Find(&records).Error
	if err != nil {
		return fmt.Errorf("查询 MySQL 数据失败: %v", err)
	}

	if len(records) == 0 {
		return nil
	}

	synced := 0
	for _, record := range records {
		// 应用记录过滤器
		if m.config.RecordFilter != nil && !m.config.RecordFilter(record) {
			continue
		}

		// 插入或更新 SQLite
		if err := m.insertOrUpdateLocal(sqlite, tableName, record); err != nil {
			logx.Errorf("插入本地记录失败 [%s]: %v", tableName, err)
			continue
		}
		synced++
	}

	if synced > 0 {
		logx.Infof("✅ 从远程同步表 [%s]: %d/%d 条记录", tableName, synced, len(records))
	}
	return nil
}

// 插入或更新远程记录
func (m *DBSyncManager) insertOrUpdateRemote(db *gorm.DB, tableName string, record map[string]interface{}) error {
	switch m.config.ConflictMode {
	case ConflictModeSkip:
		return db.Table(tableName).Create(record).Error
	case ConflictModeOverwrite:
		return db.Table(tableName).Save(record).Error
	case ConflictModeNewest:
		return m.upsertByTimestamp(db, tableName, record)
	default:
		return db.Table(tableName).Create(record).Error
	}
}

// 插入或更新本地记录
func (m *DBSyncManager) insertOrUpdateLocal(db *gorm.DB, tableName string, record map[string]interface{}) error {
	switch m.config.ConflictMode {
	case ConflictModeSkip:
		return db.Table(tableName).Create(record).Error
	case ConflictModeOverwrite:
		return db.Table(tableName).Save(record).Error
	case ConflictModeNewest:
		return m.upsertByTimestamp(db, tableName, record)
	default:
		return db.Table(tableName).Create(record).Error
	}
}

// 根据时间戳更新
func (m *DBSyncManager) upsertByTimestamp(db *gorm.DB, tableName string, record map[string]interface{}) error {
	// 检查是否有 updated_at 字段
	if _, ok := record["updated_at"]; !ok {
		// 没有时间戳，使用 Save
		return db.Table(tableName).Save(record).Error
	}

	// 查询现有记录
	var existing map[string]interface{}
	if id, ok := record["id"]; ok {
		err := db.Table(tableName).Where("id = ?", id).First(&existing).Error
		if err == gorm.ErrRecordNotFound {
			// 记录不存在，直接创建
			return db.Table(tableName).Create(record).Error
		}
		if err != nil {
			return err
		}

		// 比较时间戳
		if existingTime, ok := existing["updated_at"].(time.Time); ok {
			if recordTime, ok := record["updated_at"].(time.Time); ok {
				if recordTime.After(existingTime) {
					// 新记录更新，更新数据库
					return db.Table(tableName).Save(record).Error
				}
				// 旧记录，跳过
				return nil
			}
		}
	}

	// 默认使用 Save
	return db.Table(tableName).Save(record).Error
}

// 启动同步服务
func (m *DBSyncManager) Start() error {
	m.mu.Lock()
	if m.status == SyncStatusRunning {
		m.mu.Unlock()
		return errors.New("sync manager already running")
	}
	m.status = SyncStatusRunning
	m.mu.Unlock()

	logx.Info("🚀 启动数据库同步服务...")

	go m.syncLoop()
	return nil
}

// 同步循环
func (m *DBSyncManager) syncLoop() {
	ticker := time.NewTicker(m.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.performSync()
		case <-m.syncTrigger:
			// 手动触发同步
			m.performSync()
		}
	}
}

// 执行同步
func (m *DBSyncManager) performSync() {
	m.mu.RLock()
	if m.status == SyncStatusPaused {
		m.mu.RUnlock()
		return
	}
	m.mu.RUnlock()

	startTime := time.Now()
	logx.Info("🔄 开始数据库同步...")

	if err := m.syncAllDatabases(); err != nil {
		logx.Errorf("❌ 同步失败: %v", err)
		m.mu.Lock()
		m.status = SyncStatusError
		m.lastError = err
		m.mu.Unlock()
	}

	duration := time.Since(startTime)
	total, failed, toRemote, fromRemote, _ := m.stats.GetStats()

	if failed > 0 {
		logx.Errorf("⚠️  同步完成(有错误) - 耗时: %v, 总计: %d, 失败: %d, 上传: %d, 下载: %d",
			duration, total, failed, toRemote, fromRemote)
	} else {
		logx.Infof("✅ 同步完成 - 耗时: %v, 总计: %d, 上传: %d, 下载: %d",
			duration, total, toRemote, fromRemote)
	}
}

// 停止同步服务
func (m *DBSyncManager) Stop() error {
	m.mu.Lock()
	if m.status != SyncStatusRunning {
		m.mu.Unlock()
		return errors.New("sync manager not running")
	}
	m.mu.Unlock()

	logx.Info("🛑 停止数据库同步服务...")
	m.cancel()

	m.mu.Lock()
	m.status = SyncStatusIdle
	m.mu.Unlock()

	logx.Info("✅ 数据库同步服务已停止")
	return nil
}

// 暂停同步
func (m *DBSyncManager) Pause() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = SyncStatusPaused
	logx.Info("⏸️  暂停数据库同步")
}

// 恢复同步
func (m *DBSyncManager) Resume() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = SyncStatusRunning
	logx.Info("▶️  恢复数据库同步")
}

// 触发立即同步
func (m *DBSyncManager) TriggerSync() {
	select {
	case m.syncTrigger <- struct{}{}:
		logx.Info("🔔 触发立即同步")
	default:
		// 通道已满，说明有同步正在排队
	}
}

// 获取同步状态
func (m *DBSyncManager) GetStatus() (SyncStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.status, m.lastError
}

// 获取统计信息
func (m *DBSyncManager) GetStats() *SyncStats {
	return m.stats
}

// 清除错误
func (m *DBSyncManager) ClearError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastError = nil
	if m.status == SyncStatusError {
		m.status = SyncStatusIdle
	}
}

// 记录变更
func (m *DBSyncManager) LogChange(tableName, operation string, recordID interface{}, direction SyncDirection) {
	entry := &ChangeEntry{
		TableName: tableName,
		Operation: operation,
		RecordID:  recordID,
		Timestamp: time.Now(),
		Direction: direction,
	}
	m.changeLog.Add(entry)
}

// 变更日志
type ChangeLog struct {
	mu      sync.RWMutex
	entries []*ChangeEntry
}

type ChangeEntry struct {
	TableName string
	Operation string
	RecordID  interface{}
	Timestamp time.Time
	Direction SyncDirection
}

func NewChangeLog() *ChangeLog {
	return &ChangeLog{
		entries: make([]*ChangeEntry, 0),
	}
}

func (cl *ChangeLog) Add(entry *ChangeEntry) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	cl.entries = append(cl.entries, entry)
}

func (cl *ChangeLog) GetPending() []*ChangeEntry {
	cl.mu.RLock()
	defer cl.mu.RUnlock()
	return append([]*ChangeEntry{}, cl.entries...)
}

func (cl *ChangeLog) Remove(tableName string, recordID interface{}, direction SyncDirection) {
	cl.mu.Lock()
	defer cl.mu.Unlock()

	filtered := make([]*ChangeEntry, 0)
	for _, entry := range cl.entries {
		if entry.TableName != tableName || entry.RecordID != recordID || entry.Direction != direction {
			filtered = append(filtered, entry)
		}
	}
	cl.entries = filtered
}

func (cl *ChangeLog) Clear() {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	cl.entries = make([]*ChangeEntry, 0)
}
