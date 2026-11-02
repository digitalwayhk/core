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

// RegisterOwnedKey 只供已受信应用控制生命周期调用，外部Gateway/消息正文无此权限。
// 成功只表示技术登记；owner仍须Activate、应用SQLfence和Restore完成后Ready。
func (sc *ServiceContext) RegisterOwnedKey(ctx context.Context, m ownership.Manifest, key string) error {
	if sc == nil || sc.MQManager == nil || sc.Service == nil {
		return ownership.ErrUnsupported
	}
	if m.Service != sc.Service.Name || m.Group != m.Service {
		return ownership.ErrUnknown
	}
	return sc.MQManager.RegisterOwnershipKey(ctx, m, key)
}
