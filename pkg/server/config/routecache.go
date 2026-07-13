package config

import (
	"errors"
	"time"
)

type RouteCacheConfig struct {
	Mode  string                `json:",optional"` // off | local | shared
	TTL   time.Duration         `json:",optional"`
	L1    RouteCacheL1Config    `json:",optional"`
	L2    RouteCacheL2Config    `json:",optional"`
	Redis RouteCacheRedisConfig `json:",optional"`
}

type RouteCacheL1Config struct {
	Limit int `json:",optional"`
}

type RouteCacheL2Config struct {
	Enable           bool   `json:",optional"`
	Path             string `json:",optional"`
	MaxBytes         int64  `json:",optional"`
	CorruptionPolicy string `json:",optional"` // fail | reset
}

type RouteCacheRedisConfig struct {
	Addr          string `json:",optional"`
	Password      string `json:",optional"`
	DB            int    `json:",optional"`
	Prefix        string `json:",optional"`
	OnUnavailable string `json:",optional"` // fail | bypass
}

func (c *RouteCacheConfig) ApplyDefaults() {
	if c.Mode == "" {
		c.Mode = "off"
	}
	if c.TTL <= 0 {
		c.TTL = 10 * time.Second
	}
	if c.L1.Limit <= 0 {
		c.L1.Limit = 10000
	}
	if c.L2.MaxBytes <= 0 {
		c.L2.MaxBytes = 512 << 20
	}
	if c.L2.CorruptionPolicy == "" {
		c.L2.CorruptionPolicy = "fail"
	}
	if c.Redis.Prefix == "" {
		c.Redis.Prefix = "digitalway:routecache"
	}
	if c.Redis.OnUnavailable == "" {
		c.Redis.OnUnavailable = "fail"
	}
}

func (c *RouteCacheConfig) Validate() error {
	switch c.Mode {
	case "off", "local", "shared":
	default:
		return errors.New("routeCache.mode must be one of: off, local, shared")
	}
	if c.TTL <= 0 {
		return errors.New("routeCache.ttl must be positive")
	}
	if c.L1.Limit <= 0 {
		return errors.New("routeCache.l1.limit must be positive")
	}
	if c.L2.MaxBytes <= 0 {
		return errors.New("routeCache.l2.maxBytes must be positive")
	}
	if c.L2.CorruptionPolicy != "fail" && c.L2.CorruptionPolicy != "reset" {
		return errors.New("routeCache.l2.corruptionPolicy must be fail or reset")
	}
	if c.Redis.OnUnavailable != "fail" && c.Redis.OnUnavailable != "bypass" {
		return errors.New("routeCache.redis.onUnavailable must be fail or bypass")
	}
	if c.Mode == "shared" {
		return errors.New("routeCache.mode shared is not implemented until Redis L3 is configured")
	}
	if c.L2.Enable && c.L2.Path == "" {
		return errors.New("routeCache.l2.path is required when l2 is enabled")
	}
	if !c.L2.Enable && (c.L2.Path != "" || c.L2.MaxBytes != 512<<20 || c.L2.CorruptionPolicy != "fail") {
		return errors.New("routeCache.l2 settings require l2.enable=true")
	}
	if c.Redis.Addr != "" || c.Redis.Password != "" || c.Redis.DB != 0 ||
		c.Redis.Prefix != "digitalway:routecache" || c.Redis.OnUnavailable != "fail" {
		return errors.New("routeCache.redis is not implemented; keep the inactive defaults")
	}
	return nil
}
