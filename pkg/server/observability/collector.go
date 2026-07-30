package observability

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// ComponentCollector 将 RuntimeMetricProvider 快照导出为 Prometheus gauge。
type ComponentCollector struct {
	providers map[string][]RuntimeMetricProvider
	desc      *prometheus.Desc
	mu        sync.Mutex
}

// NewComponentCollector 创建组件采集器。service 使用逻辑服务名。
func NewComponentCollector(service string, providers []RuntimeMetricProvider) *ComponentCollector {
	svc := NormalizeServiceLabel(service)
	return &ComponentCollector{
		providers: map[string][]RuntimeMetricProvider{
			svc: append([]RuntimeMetricProvider(nil), providers...),
		},
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
	providers := make(map[string][]RuntimeMetricProvider, len(c.providers))
	for service, serviceProviders := range c.providers {
		providers[service] = append([]RuntimeMetricProvider(nil), serviceProviders...)
	}
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	for service, serviceProviders := range providers {
		for _, p := range serviceProviders {
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
}

// replaceProviders 按服务、组件名合并 Provider；同名组件使用最后一次注册值。
func (c *ComponentCollector) replaceProviders(service string, providers []RuntimeMetricProvider) {
	c.mu.Lock()
	defer c.mu.Unlock()

	replace := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		if provider != nil {
			replace[NormalizeServiceLabel(provider.ComponentName())] = struct{}{}
		}
	}
	merged := make([]RuntimeMetricProvider, 0, len(c.providers[service])+len(providers))
	for _, provider := range c.providers[service] {
		if provider == nil {
			continue
		}
		if _, replaced := replace[NormalizeServiceLabel(provider.ComponentName())]; !replaced {
			merged = append(merged, provider)
		}
	}
	for i, provider := range providers {
		if provider == nil {
			continue
		}
		name := NormalizeServiceLabel(provider.ComponentName())
		keep := true
		for j := i + 1; j < len(providers); j++ {
			if providers[j] != nil && NormalizeServiceLabel(providers[j].ComponentName()) == name {
				keep = false
				break
			}
		}
		if keep {
			merged = append(merged, provider)
		}
	}
	c.providers[service] = merged
}

// RegisterDefault 将 collector 注册到默认 registry；重复注册返回错误。
func (c *ComponentCollector) RegisterDefault() error {
	return prometheus.Register(c)
}
