// Package store 封装 07 订单服务共享远程权威库的数据访问能力。
package store

import (
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/common"
	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

var (
	remoteActionOnce sync.Once
	remoteAction     persistencetypes.IDataAction
	remoteEnsureMu   sync.Mutex
)

// GetRemote 返回所有 order 实例共享远程权威库的数据访问器。
func GetRemote() persistencetypes.IDataAction {
	remoteActionOnce.Do(func() {
		var err error
		remoteAction, err = oltp.NewDataAction(remoteStorageConfig())
		if err != nil {
			panic(err)
		}
	})
	return remoteAction
}

// remoteMySQLConfig 从环境变量创建 07 订单共享权威库配置。
func remoteMySQLConfig() *oltp.Config {
	config := &oltp.Config{
		Host:         envString("SHOP_ORDER_REMOTE_MYSQL_HOST", "127.0.0.1"),
		Port:         envInt("SHOP_ORDER_REMOTE_MYSQL_PORT", 3306),
		Username:     envString("SHOP_ORDER_REMOTE_MYSQL_USER", "root"),
		Password:     envString("SHOP_ORDER_REMOTE_MYSQL_PASSWORD", ""),
		Database:     envString("SHOP_ORDER_REMOTE_MYSQL_DATABASE", common.RemoteDatabaseName),
		Charset:      "utf8mb4",
		ParseTime:    true,
		Loc:          "Local",
		MaxIdleConns: envInt("SHOP_ORDER_REMOTE_MYSQL_MAX_IDLE", 5),
		MaxOpenConns: envInt("SHOP_ORDER_REMOTE_MYSQL_MAX_OPEN", 20),
		MaxLifetime:  30 * time.Minute,
		IsLog:        envString("SHOP_ORDER_REMOTE_MYSQL_LOG", "") == "true",
	}
	if dsn := os.Getenv("SHOP_ORDER_REMOTE_MYSQL_DSN"); dsn != "" {
		_ = config.SetMysqlDSN(dsn)
	}
	return config
}

func envString(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envInt(name string, fallback int) int {
	if value := os.Getenv(name); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

// EnsureRemoteModel 确保远程权威模型表已创建。
func EnsureRemoteModel(model interface{}) error {
	return ensureModelWith(GetRemote(), &remoteEnsureMu, model)
}

// RunRemoteTransaction 在共享远程权威库中执行独立远程事务。
func RunRemoteTransaction(ensureStorage func() error, operation func(persistencetypes.IDataAction) error) error {
	return runTransaction(nil, &remoteEnsureMu, GetRemote, ensureStorage, operation)
}

// remoteStorageConfig 仅在模型组合根选择驱动，旧 MySQL 环境变量保持兼容。
func remoteStorageConfig() persistencetypes.StorageConfig {
	driver := os.Getenv("SHOP_ORDER_REMOTE_DRIVER")
	if driver == "" {
		old := remoteMySQLConfig()
		return persistencetypes.StorageConfig{Driver: "mysql", Host: old.Host, Port: old.Port, Username: old.Username, Password: old.Password, Database: old.Database, MaxIdleConns: old.MaxIdleConns, MaxOpenConns: old.MaxOpenConns, MaxLifetime: old.MaxLifetime, IsLog: old.IsLog}
	}
	value := persistencetypes.StorageConfig{Driver: driver, Host: envString("SHOP_ORDER_REMOTE_HOST", "127.0.0.1"), Username: os.Getenv("SHOP_ORDER_REMOTE_USER"), Password: os.Getenv("SHOP_ORDER_REMOTE_PASSWORD"), Database: envString("SHOP_ORDER_REMOTE_DATABASE", common.RemoteDatabaseName), SSLMode: os.Getenv("SHOP_ORDER_REMOTE_SSL_MODE"), SSLRootCert: os.Getenv("SHOP_ORDER_REMOTE_SSL_ROOT_CERT"), MaxIdleConns: envInt("SHOP_ORDER_REMOTE_MAX_IDLE", 5), MaxOpenConns: envInt("SHOP_ORDER_REMOTE_MAX_OPEN", 20)}
	if port, err := strconv.Atoi(os.Getenv("SHOP_ORDER_REMOTE_PORT")); err == nil {
		value.Port = port
	}
	value.ApplyDefaults()
	return value
}
