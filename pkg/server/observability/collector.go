package observability

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ComponentCollector 将 RuntimeMetricProvider 快照导出为 Prometheus gauge。
type ComponentCollector struct {
	service   string
	providers []RuntimeMetricProvider
	desc      *prometheus.Desc
	mu        sync.Mutex
}

// NewComponentCollector 创建组件采集器。service 使用逻辑服务名。
func NewComponentCollector(service string, providers []RuntimeMetricProvider) *ComponentCollector {
	return &ComponentCollector{
		service:   NormalizeServiceLabel(service),
		providers: append([]RuntimeMetricProvider(nil), providers...),
		desc: prometheus.NewDesc(
			"core_component_gauge",
			"core component runtime gauge",
			[]string{"service", "component", "name", "state"},
			nil,
		),
	}
}

// Describe 实现 prometheus.Collector。
func (c *ComponentCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.desc
}

// Collect 实现 prometheus.Collector。
func (c *ComponentCollector) Collect(ch chan<- prometheus.Metric) {
	c.mu.Lock()
	providers := c.providers
	service := c.service
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	for _, p := range providers {
		if p == nil {
			continue
		}
		snap := p.RuntimeMetricSnapshot(ctx)
		component := NormalizeServiceLabel(snap.Component)
		if component == "unknown" {
			component = NormalizeServiceLabel(p.ComponentName())
		}
		state := snap.State
		if state == "" {
			state = "ok"
		}
		for name, value := range filterGauges(snap.Gauges) {
			ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, value, service, component, name, state)
		}
	}
}

// RegisterDefault 将 collector 注册到默认 registry；重复注册返回错误。
func (c *ComponentCollector) RegisterDefault() error {
	return prometheus.Register(c)
}
