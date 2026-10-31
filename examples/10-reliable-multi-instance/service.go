// Package multiinstance 用正式 ServiceContext 演示两个消费副本共享一个逻辑组。
package multiinstance

import (
	"context"
	"fmt"

	privateapi "github.com/digitalwayhk/core/examples/10-reliable-multi-instance/api/private"
	"github.com/digitalwayhk/core/examples/10-reliable-multi-instance/business"
	"github.com/digitalwayhk/core/examples/10-reliable-multi-instance/contract"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type Service struct {
	Worker bool
	stop   func()
}

func (s *Service) ServiceName() string {
	if s.Worker {
		return contract.WorkerService
	}
	return contract.ProducerService
}
func (s *Service) Routers() []types.IRouter {
	if s.Worker {
		return []types.IRouter{&privateapi.Progress{}}
	}
	return []types.IRouter{&privateapi.Submit{}}
}

// Initialize 在接受请求前声明同一生命周期并完成必需订阅，能力不足阻止启动。
func (s *Service) Initialize() error {
	sc := router.GetContext(s.ServiceName())
	if sc == nil {
		return fmt.Errorf("缺少演示服务上下文")
	}
	if err := sc.RequireMessageLifecycle(context.Background(), contract.Lifecycle()); err != nil {
		return err
	}
	if err := sc.RequireOrderedReliableByShardKey(); err != nil {
		return err
	}
	if !s.Worker {
		return nil
	}
	stop, err := sc.SubscribeEvent(event.Subscription{Subject: contract.Subject, EventType: contract.EventType, Reliable: true, BrokerOnly: true, SharedKeys: true, Prefetch: 8, KeyConcurrency: 1, Handler: business.Current.Handle})
	if err != nil {
		return err
	}
	s.stop = stop
	return nil
}
func (s *Service) Stop() {
	if s.stop != nil {
		s.stop()
	}
}
