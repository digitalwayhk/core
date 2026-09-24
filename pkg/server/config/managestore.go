// 本文件定义 Core Manage 控制面的进程级存储配置。
package config

import (
	"fmt"
	"strings"
)

const (
	// ManageStoreDriverSQLite 使用 Core 默认的本地 SQLite 控制面库。
	ManageStoreDriverSQLite = "sqlite"
	// ManageStoreDriverMySQL 使用多进程共享的 MySQL 控制面库。
	ManageStoreDriverMySQL = "mysql"
)

// ManageStoreConfig 配置目录、菜单、按钮、角色、权限、管理员主体和主体角色关系的唯一存储。
// 该配置只能出现在内置 server 服务的 server.json 中。
type ManageStoreConfig struct {
	Driver       string
	Database     string
	Host         string
	Port         int
	Username     string
	Password     string
	MaxIdleConns int
	MaxOpenConns int
}

// DefaultManageStoreConfig 返回新安装使用的 SQLite 默认配置。
func DefaultManageStoreConfig() *ManageStoreConfig {
	return &ManageStoreConfig{
		Driver:       ManageStoreDriverSQLite,
		Database:     "core_manage",
		Port:         3306,
		MaxIdleConns: 10,
		MaxOpenConns: 50,
	}
}

// legacyManageStoreConfig 保持未声明 ManageStore 的旧 server.json 继续读取 models.db。
func legacyManageStoreConfig() *ManageStoreConfig {
	value := DefaultManageStoreConfig()
	value.Database = "models"
	return value
}

// ApplyDefaults 补齐不改变存储选择的连接默认值。
func (c *ManageStoreConfig) ApplyDefaults() {
	if c == nil {
		return
	}
	c.Driver = strings.ToLower(strings.TrimSpace(c.Driver))
	if c.Driver == "" {
		c.Driver = ManageStoreDriverSQLite
	}
	c.Database = strings.TrimSpace(c.Database)
	if c.Database == "" && c.Driver == ManageStoreDriverSQLite {
		c.Database = "core_manage"
	}
	if c.Port == 0 {
		c.Port = 3306
	}
	if c.MaxIdleConns == 0 {
		c.MaxIdleConns = 10
	}
	if c.MaxOpenConns == 0 {
		c.MaxOpenConns = 50
	}
}

// Validate 拒绝未支持驱动、不完整 MySQL 配置和无效连接池。
func (c ManageStoreConfig) Validate() error {
	switch c.Driver {
	case ManageStoreDriverSQLite:
		if strings.TrimSpace(c.Database) == "" {
			return fmt.Errorf("ManageStore.Database is required")
		}
	case ManageStoreDriverMySQL:
		if strings.TrimSpace(c.Host) == "" {
			return fmt.Errorf("ManageStore.Host is required for mysql")
		}
		if c.Port <= 0 || c.Port > 65535 {
			return fmt.Errorf("ManageStore.Port must be between 1 and 65535")
		}
		if strings.TrimSpace(c.Username) == "" {
			return fmt.Errorf("ManageStore.Username is required for mysql")
		}
		if strings.TrimSpace(c.Database) == "" {
			return fmt.Errorf("ManageStore.Database is required for mysql")
		}
	default:
		return fmt.Errorf("ManageStore.Driver must be sqlite or mysql")
	}
	if c.MaxIdleConns < 0 {
		return fmt.Errorf("ManageStore.MaxIdleConns must not be negative")
	}
	if c.MaxOpenConns <= 0 {
		return fmt.Errorf("ManageStore.MaxOpenConns must be positive")
	}
	if c.MaxIdleConns > c.MaxOpenConns {
		return fmt.Errorf("ManageStore.MaxIdleConns must not exceed MaxOpenConns")
	}
	return nil
}
