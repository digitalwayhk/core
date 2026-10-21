// 本文件是ServiceContext正式持续归属接入面，运行时资源交给实例生命周期管理。
package router

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/ownership"
)

// SubscribeOwnedEvents 启动有界批次与恢复回调，失败不退到进程内事件或哈希owner。
func (sc *ServiceContext) SubscribeOwnedEvents(sub event.OwnedSubscription) (*ownership.Runtime, error) {
	if sc == nil || sc.ServiceEventBridge == nil || sc.ServiceInstanceID == "" {
		return nil, ownership.ErrUnsupported
	}
	sc.ownedMetricsMu.Lock()
	defer sc.ownedMetricsMu.Unlock()
	if sc.ownedMetrics == nil {
		sc.ownedMetrics = &ownedRuntimeMetrics{}
	}
	if sc.ownedMetrics.size() >= 64 {
		return nil, ownership.ErrCapacity
	}
	runtime, err := sc.ServiceEventBridge.SubscribeOwned(context.Background(), sub, sc.ServiceInstanceID)
	if err != nil {
		return nil, err
	}
	if err := sc.UseResource("ownership:"+sub.Manifest.Scope, runtime); err != nil {
		_ = runtime.Close(context.Background())
		return nil, err
	}
	sc.ownedMetrics.add(runtime)
	if err := sc.RegisterRuntimeMetricProviders(sc.ownedMetrics); err != nil {
		_ = runtime.Close(context.Background())
		return nil, err
	}
	return runtime, nil
}
