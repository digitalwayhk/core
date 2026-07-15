package config

import (
	"errors"
	"path/filepath"
	"strings"
)

const (
	AuthRevocationModeLocal  = "local"
	AuthRevocationModeShared = "shared"

	defaultAuthRevocationRedisPrefix = "core:authrevocation"
)

// AuthRevocationRedisConfig 配置共享认证撤销状态使用的 Redis 权威存储。
type AuthRevocationRedisConfig struct {
	Addr     string
	Password string
	Prefix   string
}

// AuthRevocationConfig 配置 Casdoor 身份撤销状态的持久化模式。
// local 使用 Badger；shared 使用 Redis 作为权威，Badger 仅保存本地确认快照。
type AuthRevocationConfig struct {
	Mode       string
	BadgerPath string
	Redis      AuthRevocationRedisConfig
}

func (c *AuthRevocationConfig) ApplyDefaults(service string) {
	if c.Mode == "" {
		c.Mode = AuthRevocationModeLocal
	}
	if c.BadgerPath == "" {
		parts := []string{"data"}
		if strings.TrimSpace(service) != "" {
			parts = append(parts, strings.ToLower(strings.TrimSpace(service)))
		}
		parts = append(parts, "auth-revocation")
		c.BadgerPath = filepath.Join(parts...)
	}
	if c.Redis.Prefix == "" {
		c.Redis.Prefix = defaultAuthRevocationRedisPrefix
	}
}

func (c AuthRevocationConfig) Validate(casdoorEnabled bool) error {
	if c.Mode != AuthRevocationModeLocal && c.Mode != AuthRevocationModeShared {
		return errors.New("authRevocation.mode must be local or shared")
	}
	if !casdoorEnabled {
		return nil
	}
	if strings.TrimSpace(c.BadgerPath) == "" {
		return errors.New("authRevocation.badgerPath is required when Casdoor is enabled")
	}
	if c.Mode == AuthRevocationModeShared && strings.TrimSpace(c.Redis.Addr) == "" {
		return errors.New("authRevocation.redis.addr is required in shared mode")
	}
	return nil
}
