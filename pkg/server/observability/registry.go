package observability

import (
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	registryMu         sync.Mutex
	componentCollector *ComponentCollector
)

// RegisterComponentProviders 将服务组件注册到进程级单例 Collector。
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

	if componentCollector == nil {
		c := NewComponentCollector(svc, nil)
		if err := prometheus.Register(c); err != nil {
			if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
				existing, ok := are.ExistingCollector.(*ComponentCollector)
				if !ok {
					return err
				}
				c = existing
			} else {
				return err
			}
		}
		componentCollector = c
	}
	componentCollector.replaceProviders(svc, providers)
	return nil
}

// ResetComponentRegistryForTest 仅测试使用。
func ResetComponentRegistryForTest() {
	registryMu.Lock()
	defer registryMu.Unlock()
	if componentCollector != nil {
		prometheus.Unregister(componentCollector)
	}
	componentCollector = nil
}
