// 本文件定义模型持久化组合根共用的存储配置，不依赖服务或业务包。
package types

import (
	"fmt"
	"strings"
	"time"
)

// StorageConfig 集中描述 OLTP 连接；空 Database 仅用于显式模型分库。
type StorageConfig struct {
	// IsLog 仅兼容原 MySQL 日志设置；新业务不应开启 SQL 明文日志。
	IsLog               bool
	Driver              string
	Database            string
	Host                string
	Port                int
	Username            string
	Password            string
	MaxIdleConns        int
	MaxOpenConns        int
	MaxLifetime         time.Duration
	ConnectTimeout      time.Duration
	SSLMode             string
	SSLRootCert         string
	SSLCert             string
	SSLKey              string
	TimeZone            string
	MaintenanceDatabase string
}

// ApplyDefaults 补齐驱动缺省值，不覆盖已声明的数据库和端口。
func (c *StorageConfig) ApplyDefaults() {
	c.Driver = strings.ToLower(strings.TrimSpace(c.Driver))
	if c.Driver == "" {
		c.Driver = "sqlite"
	}
	c.Database = strings.TrimSpace(c.Database)
	if c.Port == 0 {
		if c.Driver == "postgres" {
			c.Port = 5432
		} else {
			c.Port = 3306
		}
	}
	if c.MaxIdleConns == 0 {
		c.MaxIdleConns = 10
	}
	if c.MaxOpenConns == 0 {
		c.MaxOpenConns = 50
	}
	if c.MaxLifetime == 0 {
		c.MaxLifetime = 30 * time.Minute
	}
	if c.ConnectTimeout == 0 {
		c.ConnectTimeout = 5 * time.Second
	}
	if c.SSLMode == "" {
		c.SSLMode = "require"
	}
	if c.TimeZone == "" {
		c.TimeZone = "UTC"
	}
	if c.MaintenanceDatabase == "" {
		c.MaintenanceDatabase = "postgres"
	}
}

// Validate 校验连接字段；fixed 要求固定库，控制面必须传 true。
func (c StorageConfig) Validate(fixed bool) error {
	switch c.Driver {
	case "sqlite":
		if c.Database == "" {
			return fmt.Errorf("Database is required")
		}
	case "mysql", "postgres":
		if strings.TrimSpace(c.Host) == "" {
			return fmt.Errorf("Host is required")
		}
		if c.Port < 1 || c.Port > 65535 {
			return fmt.Errorf("Port must be between 1 and 65535")
		}
		if strings.TrimSpace(c.Username) == "" {
			return fmt.Errorf("Username is required")
		}
		if fixed && strings.TrimSpace(c.Database) == "" {
			return fmt.Errorf("Database is required")
		}
	default:
		return fmt.Errorf("Driver is unsupported: %q", c.Driver)
	}
	if c.MaxIdleConns < 0 || c.MaxOpenConns <= 0 || c.MaxIdleConns > c.MaxOpenConns {
		return fmt.Errorf("MaxIdleConns/MaxOpenConns are invalid")
	}
	if c.ConnectTimeout < 0 || c.MaxLifetime < 0 {
		return fmt.Errorf("connection durations must not be negative")
	}
	if c.Driver == "postgres" {
		switch c.SSLMode {
		case "disable", "allow", "prefer", "require", "verify-ca", "verify-full":
		default:
			return fmt.Errorf("SSLMode is invalid")
		}
		if (c.SSLCert == "") != (c.SSLKey == "") {
			return fmt.Errorf("SSLCert and SSLKey must be configured together")
		}
		if _, err := time.LoadLocation(c.TimeZone); err != nil {
			return fmt.Errorf("TimeZone is invalid")
		}
	}
	return nil
}
