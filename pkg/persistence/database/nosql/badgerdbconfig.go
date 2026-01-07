package nosql

import (
	"fmt"
	"time"
)

// BadgerDBConfig BadgerDB 配置
type BadgerDBConfig struct {
	// 基础配置
	Path string `json:"path" yaml:"path"` // 数据库路径

	// 性能模式
	Mode string `json:"mode" yaml:"mode"` // "production" 或 "fast"

	// BadgerDB 原生配置
	SyncWrites         bool  `json:"sync_writes" yaml:"sync_writes"`                     // 同步写入（默认 true）
	DetectConflicts    bool  `json:"detect_conflicts" yaml:"detect_conflicts"`           // 检测冲突（默认 true）
	NumCompactors      int   `json:"num_compactors" yaml:"num_compactors"`               // 压缩器数量（默认 4）
	ValueLogFileSize   int64 `json:"value_log_file_size" yaml:"value_log_file_size"`     // ValueLog 文件大小（默认 128MB）
	MemTableSize       int64 `json:"mem_table_size" yaml:"mem_table_size"`               // MemTable 大小（默认 64MB）
	NumLevelZeroTables int   `json:"num_level_zero_tables" yaml:"num_level_zero_tables"` // L0 表数量（默认 4）
	NumLevelZeroStall  int   `json:"num_level_zero_stall" yaml:"num_level_zero_stall"`   // L0 阻塞数量（默认 8）
	ValueThreshold     int64 `json:"value_threshold" yaml:"value_threshold"`             // 值阈值（默认 1024）
	EnableLogger       bool  `json:"enable_logger" yaml:"enable_logger"`                 // 启用日志（默认 true）

	// 同步配置
	AutoSync        bool          `json:"auto_sync" yaml:"auto_sync"`                 // 是否自动同步到其他DB（默认 false）
	SyncInterval    time.Duration `json:"sync_interval" yaml:"sync_interval"`         // 同步间隔（默认 1s）
	SyncBatchSize   int           `json:"sync_batch_size" yaml:"sync_batch_size"`     // 同步批次大小（默认 1000）
	SyncMinInterval time.Duration `json:"sync_min_interval" yaml:"sync_min_interval"` // 最小同步间隔（默认 1s）
	SyncMaxInterval time.Duration `json:"sync_max_interval" yaml:"sync_max_interval"` // 最大同步间隔（默认 10s）

	// 清理配置
	AutoCleanup     bool          `json:"auto_cleanup" yaml:"auto_cleanup"`         // 是否自动清理（默认 false）
	CleanupInterval time.Duration `json:"cleanup_interval" yaml:"cleanup_interval"` // 清理检查间隔（默认 1h）
	KeepDuration    time.Duration `json:"keep_duration" yaml:"keep_duration"`       // 数据保留时长（默认 24h）
	SizeThreshold   int64         `json:"size_threshold" yaml:"size_threshold"`     // 触发清理的大小阈值（默认 1GB）

	// GC 配置
	GCInterval     time.Duration `json:"gc_interval" yaml:"gc_interval"`           // GC 间隔（默认 5min）
	GCDiscardRatio float64       `json:"gc_discard_ratio" yaml:"gc_discard_ratio"` // GC 丢弃比例（默认 0.5）

	// 磁盘同步配置（Fast 模式）
	PeriodicSync         bool          `json:"periodic_sync" yaml:"periodic_sync"`                   // 是否定期同步到磁盘（默认 false）
	PeriodicSyncInterval time.Duration `json:"periodic_sync_interval" yaml:"periodic_sync_interval"` // 磁盘同步间隔（默认 1s）
}

// DefaultProductionConfig 生产环境默认配置
func DefaultProductionConfig(path string) BadgerDBConfig {
	return BadgerDBConfig{
		// 基础配置
		Path: path,
		Mode: "production",

		// BadgerDB 配置
		SyncWrites:         true,
		DetectConflicts:    true,
		NumCompactors:      4,
		ValueLogFileSize:   128 << 20, // 128MB
		MemTableSize:       64 << 20,  // 64MB
		NumLevelZeroTables: 4,
		NumLevelZeroStall:  8,
		ValueThreshold:     1024,
		EnableLogger:       true,

		// 同步配置
		AutoSync:        false,
		SyncInterval:    1 * time.Second,
		SyncBatchSize:   1000,
		SyncMinInterval: 1 * time.Second,
		SyncMaxInterval: 10 * time.Second,

		// 清理配置
		AutoCleanup:     false,
		CleanupInterval: 1 * time.Hour,
		KeepDuration:    24 * time.Hour,
		SizeThreshold:   1 << 30, // 1GB

		// GC 配置
		GCInterval:     5 * time.Minute,
		GCDiscardRatio: 0.5,

		// 磁盘同步配置
		PeriodicSync:         false,
		PeriodicSyncInterval: 500 * time.Millisecond,
	}
}

// DefaultFastConfig 快速模式默认配置
func DefaultFastConfig(path string) BadgerDBConfig {
	return BadgerDBConfig{
		// 基础配置
		Path: path,
		Mode: "fast",

		// BadgerDB 配置
		SyncWrites:         false,
		DetectConflicts:    false,
		NumCompactors:      2,
		ValueLogFileSize:   64 << 20, // 64MB
		MemTableSize:       8 << 20,  // 8MB
		NumLevelZeroTables: 2,
		NumLevelZeroStall:  4,
		ValueThreshold:     1024,
		EnableLogger:       false,

		// 同步配置
		AutoSync:        false,
		SyncInterval:    1 * time.Second,
		SyncBatchSize:   1000,
		SyncMinInterval: 500 * time.Millisecond,
		SyncMaxInterval: 5 * time.Second,

		// 清理配置
		AutoCleanup:     false,
		CleanupInterval: 30 * time.Minute,
		KeepDuration:    12 * time.Hour,
		SizeThreshold:   512 << 20, // 512MB

		// GC 配置
		GCInterval:     10 * time.Minute,
		GCDiscardRatio: 0.5,

		// 磁盘同步配置
		PeriodicSync:         true,
		PeriodicSyncInterval: 500 * time.Millisecond,
	}
}

// Validate 验证配置
func (c *BadgerDBConfig) Validate() error {
	if c.Path == "" {
		return fmt.Errorf("path 不能为空")
	}

	if c.SyncBatchSize <= 0 {
		c.SyncBatchSize = 1000
	}

	if c.SyncMinInterval <= 0 {
		c.SyncMinInterval = 1 * time.Second
	}

	if c.SyncMaxInterval <= 0 {
		c.SyncMaxInterval = 10 * time.Second
	}

	if c.CleanupInterval <= 0 {
		c.CleanupInterval = 1 * time.Hour
	}

	if c.KeepDuration <= 0 {
		c.KeepDuration = 24 * time.Hour
	}

	if c.SizeThreshold <= 0 {
		c.SizeThreshold = 1 << 30 // 1GB
	}

	if c.GCInterval <= 0 {
		c.GCInterval = 5 * time.Minute
	}

	if c.GCDiscardRatio <= 0 || c.GCDiscardRatio > 1 {
		c.GCDiscardRatio = 0.5
	}

	return nil
}
