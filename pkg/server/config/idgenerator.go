// 本文件定义独立于服务发现的稳定身份与编号权威配置。
package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var identitySegment = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

// IDGeneratorConfig 显式配置编号权威，不能继承服务发现的自动降级。
type IDGeneratorConfig struct {
	LocalPath         string                `json:",optional"`
	Provider          string                `json:",optional"`
	Namespace         string                `json:",optional"`
	Epoch             string                `json:",optional"`
	Providers         ClusterProviderConfig `json:",optional"`
	LocalDevelopment  bool                  `json:",optional"`
	ReservationWindow time.Duration         `json:",optional"`
	StartupTimeout    time.Duration         `json:",optional"`
}

func (c *ServerConfig) validateIdentity() error {
	mode := c.Cluster.IdentityMode
	if mode == "" || mode == "legacy" {
		if c.IDGenerator != nil || c.Cluster.IdentityFile != "" || c.Cluster.InstanceID != "" {
			return fmt.Errorf("stable identity fields require Cluster.IdentityMode=stable")
		}
		return nil
	}
	if mode != "stable" {
		return fmt.Errorf("unsupported Cluster.IdentityMode %q", mode)
	}
	if !filepath.IsAbs(c.Cluster.IdentityFile) {
		return fmt.Errorf("stable identity requires an absolute Cluster.IdentityFile")
	}
	if c.Cluster.InstanceID != "" && !identitySegment.MatchString(c.Cluster.InstanceID) {
		return fmt.Errorf("invalid Cluster.InstanceID")
	}
	if c.Cluster.Mode != "off" && c.Cluster.Provider != "local" && c.Cluster.Provider != "redis" && c.Cluster.Provider != "etcd" {
		return fmt.Errorf("stable discovery provider %q is unsupported", c.Cluster.Provider)
	}
	if c.Cluster.Claim.MachineIDMax > 31 {
		return fmt.Errorf("stable MachineIDMax must not exceed 31")
	}
	if c.Cluster.Mode != "off" {
		ttl := c.Cluster.HeartbeatTimeout
		if c.Cluster.Provider == "redis" {
			ttl = c.Cluster.Providers.Redis.TTL
		}
		if c.Cluster.Provider == "etcd" {
			ttl = c.Cluster.Providers.Etcd.TTL
		}
		if ttl < time.Second || c.Cluster.HeartbeatInterval <= 0 || c.Cluster.HeartbeatInterval >= ttl*4/5 {
			return fmt.Errorf("stable discovery requires TTL >= 1s and heartbeat interval below 80%% of TTL")
		}
	}
	return c.IDGenerator.Validate()
}

// Validate 校验独立编号配置，也供离线工具和构造器直接调用。
func (g *IDGeneratorConfig) Validate() error {
	if g == nil {
		return fmt.Errorf("stable identity requires independent IDGenerator configuration")
	}
	if !identitySegment.MatchString(g.Namespace) || !identitySegment.MatchString(g.Epoch) {
		return fmt.Errorf("IDGenerator.Namespace and Epoch must be explicit safe identifiers")
	}
	if g.Providers.Consul.Address != "" || g.Providers.Consul.Prefix != "" || g.Providers.Consul.TTL != 0 {
		return fmt.Errorf("IDGenerator Consul is unsupported")
	}
	if g.Providers.Redis.TTL != 0 || g.Providers.Etcd.TTL != 0 {
		return fmt.Errorf("IDGenerator bindings cannot have discovery TTL")
	}
	if g.ReservationWindow > 0 && g.ReservationWindow < time.Millisecond {
		return fmt.Errorf("IDGenerator reservation window must be at least 1ms")
	}
	if g.ReservationWindow < 0 || g.ReservationWindow > time.Minute || g.StartupTimeout < 0 || g.StartupTimeout > 5*time.Minute {
		return fmt.Errorf("invalid IDGenerator time budget")
	}
	switch g.Provider {
	case "local":
		if !filepath.IsAbs(g.LocalPath) {
			return fmt.Errorf("local IDGenerator requires an absolute LocalPath")
		}
		if !g.LocalDevelopment {
			return fmt.Errorf("local IDGenerator requires LocalDevelopment=true")
		}
	case "redis":
		if strings.TrimSpace(g.Providers.Redis.Addr) == "" || g.Providers.Redis.DB < 0 {
			return fmt.Errorf("IDGenerator redis address is required")
		}
	case "etcd":
		if len(g.Providers.Etcd.Endpoints) == 0 {
			return fmt.Errorf("IDGenerator etcd endpoints are required")
		}
	default:
		return fmt.Errorf("unsupported IDGenerator provider %q", g.Provider)
	}
	return nil
}

// ApplyDefaults 只补编号时间预算，不导入发现 Provider 的 TTL 或降级配置。
func (g *IDGeneratorConfig) ApplyDefaults() {
	if g.Namespace == "" {
		g.Namespace = "core"
	}
	if g.ReservationWindow == 0 {
		g.ReservationWindow = time.Second
	}
	if g.StartupTimeout == 0 {
		g.StartupTimeout = 5 * time.Second
	}
}
