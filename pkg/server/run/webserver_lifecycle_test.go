package run

import (
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/zeromicro/go-zero/core/service"
)

var _ types.IApplicationServer = (*WebServer)(nil)

type lifecycleGroupServer struct {
	started  chan struct{}
	stopped  chan struct{}
	stopOnce sync.Once
}

func (s *lifecycleGroupServer) Start() {
	close(s.started)
	<-s.stopped
}

func (s *lifecycleGroupServer) Stop() {
	s.stopOnce.Do(func() { close(s.stopped) })
}

type lifecycleBusinessService struct {
	name        string
	stopStarted chan struct{}
	stopRelease chan struct{}
	stopOnce    sync.Once
}

func (s *lifecycleBusinessService) ServiceName() string      { return s.name }
func (s *lifecycleBusinessService) Routers() []types.IRouter { return nil }
func (s *lifecycleBusinessService) Stop() {
	s.stopOnce.Do(func() {
		close(s.stopStarted)
		<-s.stopRelease
	})
}

func TestWebServerStopWaitsForGroupAndBusinessStop(t *testing.T) {
	webServer := bareWebServer()
	business := &lifecycleBusinessService{
		name:        "webserver-lifecycle",
		stopStarted: make(chan struct{}),
		stopRelease: make(chan struct{}),
	}
	webServer.serviceContexts[business.name] = &router.ServiceContext{
		Service: &types.Service{Name: business.name, Instance: business},
	}

	groupServer := &lifecycleGroupServer{
		started: make(chan struct{}),
		stopped: make(chan struct{}),
	}
	group := service.NewServiceGroup()
	group.Add(groupServer)
	webServer.prepareRunLifecycle()
	webServer.runStarted.Store(true)
	go webServer.runServiceGroup(group)
	waitForLifecycleSignal(t, groupServer.started, "ServiceGroup Start")

	stopDone := make(chan struct{})
	go func() {
		webServer.Stop()
		close(stopDone)
	}()
	waitForLifecycleSignal(t, business.stopStarted, "业务 Stop")
	select {
	case <-stopDone:
		t.Fatal("WebServer.Stop 在业务 Stop 完成前返回")
	default:
	}

	close(business.stopRelease)
	waitForLifecycleSignal(t, stopDone, "WebServer.Stop")

	repeatedStopDone := make(chan struct{})
	go func() {
		webServer.Stop()
		close(repeatedStopDone)
	}()
	waitForLifecycleSignal(t, repeatedStopDone, "重复 WebServer.Stop")
}

func waitForLifecycleSignal(t *testing.T, signal <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("等待 %s 超时", name)
	}
}
