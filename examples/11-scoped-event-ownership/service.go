// Package scopedownership 用正式Core持续归属、SQLfence及现有WS机制演示恢复闭环。
package scopedownership

import (
	"fmt"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/api/dto"
	privateapi "github.com/digitalwayhk/core/examples/11-scoped-event-ownership/api/private"
	publicapi "github.com/digitalwayhk/core/examples/11-scoped-event-ownership/api/public"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/business"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/contract"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// Service 的Gateway不启动可靠消费者，处理副本由Core分担有限归属键。
type Service struct {
	Runtime  *business.Runtime
	Worker   bool
	MaxOwned int
}

// ServiceName 返回所有副本共用的稳定逻辑服务名。
func (*Service) ServiceName() string { return contract.ServiceName }

// Routers 注册同一路由模型，内部Snapshot只对有效owner开放。
func (s *Service) Routers() []types.IRouter {
	return []types.IRouter{privateapi.NewView(s.Runtime), privateapi.NewSubmit(s.Runtime), publicapi.NewSnapshot(s.Runtime)}
}

// Initialize 在监听前完成存储初始化与正式订阅，不自动重建Broker归属元数据。
func (s *Service) Initialize() error {
	if err := s.Runtime.Store.EnsureStorage(); err != nil {
		return err
	}
	sc := router.GetContext(contract.ServiceName)
	if sc == nil {
		return fmt.Errorf("缺少ServiceContext")
	}
	s.Runtime.Context = sc
	if err := sc.UseResource("demo11-sql", s.Runtime.Store); err != nil {
		return err
	}
	s.Runtime.Notify = func(value *dto.Snapshot) { privateapi.NewView(s.Runtime).RouterInfo().NoticeWebSocket(value) }
	if !s.Worker {
		return nil
	}
	runtime, err := sc.SubscribeOwnedEvents(event.OwnedSubscription{Manifest: contract.Manifest(), MaxOwned: s.MaxOwned, NoDueTasks: true, Key: s.Runtime.Key, Activate: s.Runtime.Store.Activate, Restore: s.Runtime.Restore, Handler: s.Runtime.Handle})
	if err != nil {
		return err
	}
	s.Runtime.Owned = runtime
	return nil
}
