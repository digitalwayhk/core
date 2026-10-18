// Package store 封装 07 订单服务共享远程权威库的数据访问能力。
package store

import (
	"errors"
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
	remoteActionOnce.Do(func() { remoteAction = oltp.NewMySQL(remoteMySQLConfig()) })
	return remoteAction
}

// remoteMySQLConfig 从环境变量创建 07 订单共享权威库配置。
func remoteMySQLConfig() *oltp.Config {
	config := &oltp.Config{
		Host:              envString("SHOP_ORDER_REMOTE_MYSQL_HOST", "127.0.0.1"),
		Port:              envInt("SHOP_ORDER_REMOTE_MYSQL_PORT", 3306),
		Username:          envString("SHOP_ORDER_REMOTE_MYSQL_USER", "root"),
		Password:          envString("SHOP_ORDER_REMOTE_MYSQL_PASSWORD", ""),
		Database:          envString("SHOP_ORDER_REMOTE_MYSQL_DATABASE", common.RemoteDatabaseName),
		Charset:           "utf8mb4",
		InterpolateParams: true,
		ParseTime:         true,
		Loc:               "Local",
		MaxIdleConns:      envInt("SHOP_ORDER_REMOTE_MYSQL_MAX_IDLE", 20),
		MaxOpenConns:      envInt("SHOP_ORDER_REMOTE_MYSQL_MAX_OPEN", 20),
		MaxLifetime:       30 * time.Minute,
		IsLog:             envString("SHOP_ORDER_REMOTE_MYSQL_LOG", "") == "true",
	}
	if dsn := os.Getenv("SHOP_ORDER_REMOTE_MYSQL_DSN"); dsn != "" {
		config.InterpolateParams = false // 完整DSN未声明时保持旧协议。
		if err := config.SetMysqlDSN(dsn); err != nil {
			panic("无效的 SHOP_ORDER_REMOTE_MYSQL_DSN 配置")
		}
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

// RunRemoteTransaction 在共享远程权威库中执行独立 MySQL 事务。
func RunRemoteTransaction(ensureStorage func() error, operation func(persistencetypes.IDataAction) error) error {
	return runTransaction(nil, &remoteEnsureMu, GetRemote, ensureStorage, operation)
}

// RunRemoteRead 为单条只读查询克隆独立 action，不开启事务；表须在启动时建好。
func RunRemoteRead(operation func(persistencetypes.IDataAction) error) error {
	remoteEnsureMu.Lock()
	cloner, ok := GetRemote().(interface {
		Clone() persistencetypes.IDataAction
	})
	if !ok {
		remoteEnsureMu.Unlock()
		return errors.New("数据操作器不支持独立读取")
	}
	action := cloner.Clone()
	remoteEnsureMu.Unlock()
	return operation(action)
}
