// Package nosql 定义 ReliableWriteStore 的服务身份、批提交、背压和关闭配置。
package nosql

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	// ErrInvalidReliableWriteConfig 表示可靠写入配置或服务实例身份无效。
	ErrInvalidReliableWriteConfig = errors.New("可靠写入配置无效")
	serviceResourceSegment        = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
)

// ServiceIdentity 描述可靠本地存储所属的逻辑服务和已领取实例编号。
type ServiceIdentity struct {
	ServiceName  string
	DataCenterID int64
	MachineID    int64
}

// BatchCommitConfig 配置跨请求 Group Commit 的批次和排队边界。
type BatchCommitConfig struct {
	MaxBatch       int
	CollectWindow  time.Duration
	CollectBacklog int
	QueueCapacity  int
}

// WriteAdmissionConfig 配置可靠写入的并发、积压和磁盘背压阈值。
type WriteAdmissionConfig struct {
	MaxConcurrent      int
	AcquireTimeout     time.Duration
	SoftPending        int
	HardPending        int
	MaxBacklogDuration time.Duration
	HardDiskBytes      int64
}

// ReliableWriteStoreConfig 组合可靠 store 的本地路径、Badger、批提交和背压配置。
type ReliableWriteStoreConfig struct {
	BasePath     string
	Badger       BadgerDBConfig
	Batch        BatchCommitConfig
	Admission    WriteAdmissionConfig
	CloseTimeout time.Duration
}

// BatchWriteResult 描述一个有序本地批次已成功提交的前缀数量。
type BatchWriteResult struct {
	Committed int
}

// LocalScanOptions 描述 ReliableWriteStore 的本地前缀扫描条件。
type LocalScanOptions struct {
	Prefix string
	Limit  int
}

func (config ReliableWriteStoreConfig) normalized(identity ServiceIdentity) (ReliableWriteStoreConfig, error) {
	path, err := resolveReliableWritePath(config.BasePath, identity)
	if err != nil {
		return ReliableWriteStoreConfig{}, err
	}
	if config.Badger.Mode == "" {
		config.Badger = DefaultProductionConfig(path)
	}
	config.BasePath = path
	config.Badger.Path = path
	if config.Batch.MaxBatch <= 0 {
		config.Batch.MaxBatch = 128
	}
	if config.Batch.CollectWindow <= 0 {
		config.Batch.CollectWindow = time.Millisecond
	}
	if config.Batch.MaxBatch > 1 && config.Batch.CollectBacklog <= 0 {
		config.Batch.CollectBacklog = 16
		if config.Batch.CollectBacklog >= config.Batch.MaxBatch {
			config.Batch.CollectBacklog = config.Batch.MaxBatch - 1
		}
	}
	if config.Batch.CollectBacklog >= config.Batch.MaxBatch && config.Batch.MaxBatch > 1 {
		return ReliableWriteStoreConfig{}, fmt.Errorf("%w: collect_backlog 必须小于 max_batch", ErrInvalidReliableWriteConfig)
	}
	if config.Batch.QueueCapacity <= 0 {
		config.Batch.QueueCapacity = config.Batch.MaxBatch * 8
	}
	if config.Batch.QueueCapacity < config.Batch.MaxBatch {
		return ReliableWriteStoreConfig{}, fmt.Errorf("%w: batch queue_capacity 不能小于 max_batch", ErrInvalidReliableWriteConfig)
	}
	if config.CloseTimeout <= 0 {
		config.CloseTimeout = 10 * time.Second
	}
	return config, nil
}

func resolveReliableWritePath(basePath string, identity ServiceIdentity) (string, error) {
	basePath = strings.TrimSpace(basePath)
	if basePath == "" {
		return "", fmt.Errorf("%w: base_path 不能为空", ErrInvalidReliableWriteConfig)
	}
	serviceName := strings.ToLower(strings.TrimSpace(identity.ServiceName))
	if !serviceResourceSegment.MatchString(serviceName) || serviceName == "." || serviceName == ".." {
		return "", fmt.Errorf("%w: service_name=%q 不是安全目录片段", ErrInvalidReliableWriteConfig, identity.ServiceName)
	}
	if identity.DataCenterID < 0 {
		return "", fmt.Errorf("%w: data_center_id 不能为负数", ErrInvalidReliableWriteConfig)
	}
	if identity.MachineID < 0 {
		return "", fmt.Errorf("%w: machine_id 不能为负数", ErrInvalidReliableWriteConfig)
	}
	return filepath.Join(
		filepath.Clean(basePath),
		serviceName,
		fmt.Sprintf("dc-%d", identity.DataCenterID),
		fmt.Sprintf("machine-%d", identity.MachineID),
	), nil
}
