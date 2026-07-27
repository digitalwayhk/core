package observability

import (
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	registryMu  sync.Mutex
	collectors  = map[string]*ComponentCollector{}
)

// RegisterComponentProviders 按服务名隔离注册本进程组件 Collector。
func RegisterComponentProviders(service string, providers ...RuntimeMetricProvider) error {
	if len(providers) == 0 {
		return fmt.Errorf("no runtime metric providers")
	}
	svc := NormalizeServiceLabel(service)
	if svc == "unknown" {
		return fmt.Errorf("invalid service for runtime metric providers")
	}
	registryMu.Lock()
	defer registryMu.Unlock()

	merged := providers
	if old := collectors[svc]; old != nil {
		old.mu.Lock()
		merged = append(append([]RuntimeMetricProvider(nil), old.providers...), providers...)
		old.mu.Unlock()
		prometheus.Unregister(old)
	}
	c := NewComponentCollector(svc, merged)
	if err := prometheus.Register(c); err != nil {
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			if existing, ok := are.ExistingCollector.(*ComponentCollector); ok {
				prometheus.Unregister(existing)
				if err2 := prometheus.Register(c); err2 != nil {
					return err2
				}
			} else {
				return err
			}
		} else {
			return err
		}
	}
	collectors[svc] = c
	return nil
}

// ResetComponentRegistryForTest 仅测试使用。
func ResetComponentRegistryForTest() {
	registryMu.Lock()
	defer registryMu.Unlock()
	for _, c := range collectors {
		prometheus.Unregister(c)
	}
	collectors = map[string]*ComponentCollector{}
}
