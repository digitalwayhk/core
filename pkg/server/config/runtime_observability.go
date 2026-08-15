package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// RuntimeObservabilityConfig 控制 ServerManage 侧 Runtime Aggregator 的指标查询。
// 业务实例只需暴露 Prometheus scrape；QueryURL 仅查询端需要。
type RuntimeObservabilityConfig struct {
	// Mode: off | prometheus。默认值由 ApplyDefaults 写入，避免 go-zero conf 嵌套 default 干扰迁移加载。
	Mode                 string        `json:",optional"`
	QueryURL             string        `json:",optional"`
	QueryTimeout         time.Duration `json:",optional"`
	MaxConcurrentQueries int           `json:",optional"`
	CacheTTL             time.Duration `json:",optional"`
}

// ApplyDefaults 补充 RuntimeObservability 默认值，并规范化 Mode。
func (c *RuntimeObservabilityConfig) ApplyDefaults() {
	c.Mode = strings.ToLower(strings.TrimSpace(c.Mode))
	if c.Mode == "" {
		c.Mode = "off"
	}
	if c.QueryTimeout <= 0 {
		c.QueryTimeout = 3 * time.Second
	}
	if c.MaxConcurrentQueries <= 0 {
		c.MaxConcurrentQueries = 4
	}
	if c.CacheTTL <= 0 {
		c.CacheTTL = 5 * time.Second
	}
}

// Validate 校验 RuntimeObservability 合法性。
// 注意：go-zero conf.MustLoad 会在 ApplyDefaults 之前调用 Validate，
// 因此空 Mode / 零超时视为“尚未补默认值”，按 off 语义放行。
func (c RuntimeObservabilityConfig) Validate() error {
	mode := strings.ToLower(strings.TrimSpace(c.Mode))
	if mode == "" {
		mode = "off"
	}
	switch mode {
	case "off", "prometheus":
	default:
		return fmt.Errorf("RuntimeObservability.Mode must be off or prometheus, got %q", c.Mode)
	}
	if c.QueryTimeout < 0 {
		return errors.New("RuntimeObservability.QueryTimeout must not be negative")
	}
	if c.MaxConcurrentQueries < 0 {
		return errors.New("RuntimeObservability.MaxConcurrentQueries must not be negative")
	}
	if c.MaxConcurrentQueries > 32 {
		return errors.New("RuntimeObservability.MaxConcurrentQueries must be <= 32")
	}
	if c.CacheTTL < 0 {
		return errors.New("RuntimeObservability.CacheTTL must not be negative")
	}
	if mode == "prometheus" {
		raw := strings.TrimSpace(c.QueryURL)
		if raw == "" {
			return errors.New("RuntimeObservability.QueryURL is required when Mode=prometheus")
		}
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return errors.New("RuntimeObservability.QueryURL is invalid")
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return errors.New("RuntimeObservability.QueryURL scheme must be http or https")
		}
	}
	return nil
}

// RedactedQueryURLHost 返回仅 host 的安全摘要（日志用，不含 userinfo/path）。
func (c RuntimeObservabilityConfig) RedactedQueryURLHost() string {
	raw := strings.TrimSpace(c.QueryURL)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "[redacted]"
	}
	return u.Scheme + "://" + u.Hostname()
}
