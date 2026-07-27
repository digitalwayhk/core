package observability

import (
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	registryMu       sync.Mutex
	registeredOnce   bool
	activeCollector  *ComponentCollector
)

// RegisterComponentProviders 注册本进程组件 Collector（幂等：重复调用追加 provider 并重建 collector）。
func RegisterComponentProviders(service string, providers ...RuntimeMetricProvider) error {
	if len(providers) == 0 {
		return fmt.Errorf("no runtime metric providers")
	}
	registryMu.Lock()
	defer registryMu.Unlock()

	merged := providers
	if activeCollector != nil {
		// 追加到已有列表
		activeCollector.mu.Lock()
		merged = append(append([]RuntimeMetricProvider(nil), activeCollector.providers...), providers...)
		activeCollector.mu.Unlock()
		prometheus.Unregister(activeCollector)
	}
	c := NewComponentCollector(service, merged)
	if err := prometheus.Register(c); err != nil {
		// AlreadyRegistered 时尝试替换
		if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
			if old, ok := are.ExistingCollector.(*ComponentCollector); ok {
				prometheus.Unregister(old)
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
	activeCollector = c
	registeredOnce = true
	return nil
}

// ResetComponentRegistryForTest 仅测试使用。
func ResetComponentRegistryForTest() {
	registryMu.Lock()
	defer registryMu.Unlock()
	if activeCollector != nil {
		prometheus.Unregister(activeCollector)
	}
	activeCollector = nil
	registeredOnce = false
}
