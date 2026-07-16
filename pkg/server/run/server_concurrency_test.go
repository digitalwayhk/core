package run

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/stretchr/testify/require"
)

type failingGRPCLifecycle struct {
	ready chan struct{}
	done  chan struct{}
	err   atomic.Pointer[error]
}

func newFailingGRPCLifecycle() *failingGRPCLifecycle {
	ready := make(chan struct{})
	close(ready)
	return &failingGRPCLifecycle{ready: ready, done: make(chan struct{})}
}

func (s *failingGRPCLifecycle) Start()                            { <-s.done }
func (s *failingGRPCLifecycle) Stop()                             {}
func (s *failingGRPCLifecycle) Ready() <-chan struct{}            { return s.ready }
func (s *failingGRPCLifecycle) Done() <-chan struct{}             { return s.done }
func (s *failingGRPCLifecycle) BeginShutdown()                    {}
func (s *failingGRPCLifecycle) StopContext(context.Context) error { return nil }
func (s *failingGRPCLifecycle) Err() error {
	if value := s.err.Load(); value != nil {
		return *value
	}
	return nil
}
func (s *failingGRPCLifecycle) Fail(err error) {
	s.err.Store(&err)
	close(s.done)
}

func TestWebServerStopsWhenGRPCRuntimeFails(t *testing.T) {
	service := &concurrencyTestService{name: "grpc-runtime-failure", started: make(chan struct{}, 1)}
	ctx := newConcurrencyTestContext(service)
	server := newFailingGRPCLifecycle()
	ctx.SetGRPCServer(server)
	webServer := bareWebServer()
	webServer.AddServiceContext(ctx)

	server.Fail(errors.New("serve failed"))

	require.Eventually(t, webServer.stopped.Load, callbackTimeout, time.Millisecond)
}

func TestGRPCPortOverride(t *testing.T) {
	tests := []struct {
		name    string
		base    int
		index   int
		want    int
		wantErr bool
	}{
		{name: "zero keeps service config", base: 0, index: 1, want: 0},
		{name: "explicit first service", base: 19090, index: 0, want: 19090},
		{name: "explicit second service", base: 19090, index: 1, want: 19091},
		{name: "overflow fails closed", base: 65535, index: 1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := grpcPortOverride(tt.base, tt.index)
			if tt.wantErr {
				if err == nil {
					t.Fatal("期望端口溢出错误")
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("grpcPortOverride() = %d, %v; want %d", got, err, tt.want)
			}
		})
	}
}

func TestPrecomputeServicePortsUsesStableServiceOrder(t *testing.T) {
	alpha := newConcurrencyTestContext(&concurrencyTestService{name: "alpha", started: make(chan struct{}, 1)})
	zulu := newConcurrencyTestContext(&concurrencyTestService{name: "zulu", started: make(chan struct{}, 1)})
	alpha.Config.DataCenterID = 7
	zulu.Config.DataCenterID = 7
	alpha.Config.Port, zulu.Config.Port = 21001, 21002
	alpha.Config.SocketPort, zulu.Config.SocketPort = 0, 0

	ordered, err := precomputeServicePorts([]*router.ServiceContext{zulu, alpha}, 29090)
	require.NoError(t, err)
	require.Equal(t, []string{"alpha", "zulu"}, []string{ordered[0].Service.Name, ordered[1].Service.Name})
	require.Equal(t, 29090, alpha.Config.Transport.GRPC.Port)
	require.Equal(t, 29091, zulu.Config.Transport.GRPC.Port)
}

func TestPrecomputeServicePortsRejectsDuplicatesBeforeListening(t *testing.T) {
	first := newConcurrencyTestContext(&concurrencyTestService{name: "dup-a", started: make(chan struct{}, 1)})
	second := newConcurrencyTestContext(&concurrencyTestService{name: "dup-b", started: make(chan struct{}, 1)})
	first.Config.Port, second.Config.Port = 22001, 22002
	first.Config.Transport.GRPC.Port = 29090
	second.Config.Transport.GRPC.Port = 29090

	_, err := precomputeServicePorts([]*router.ServiceContext{second, first}, 0)
	require.ErrorContains(t, err, "duplicate gRPC port 29090")
}

func TestNewInternalServerFailsClosedWhenMTLSFilesAreMissing(t *testing.T) {
	service := &concurrencyTestService{name: "grpc-mtls-missing", started: make(chan struct{}, 1)}
	ctx := newConcurrencyTestContext(service)
	ctx.Config.Host = "127.0.0.1"
	ctx.Config.Transport.GRPC.Port = 0
	ctx.Config.Transport.GRPC.Security = config.GRPCSecurityConfig{
		Mode: "mtls", CAFile: "missing-ca.pem", CertFile: "missing-cert.pem", KeyFile: "missing-key.pem",
	}

	err := bareWebServer().newInternalServer(ctx)
	if err == nil || !strings.Contains(err.Error(), "Transport.GRPC.Security.") {
		t.Fatalf("缺失 mTLS 文件必须使构造失败，得到 %v", err)
	}
}

const callbackTimeout = 2 * time.Second

var _ sync.Locker = (*WebServer)(nil)

type concurrencyTestService struct {
	name    string
	started chan struct{}
}

func (s *concurrencyTestService) ServiceName() string { return s.name }
func (s *concurrencyTestService) Routers() []types.IRouter {
	return nil
}
func (s *concurrencyTestService) SubscribeRouters() []*types.ObserveArgs {
	return nil
}
func (s *concurrencyTestService) Start() {
	select {
	case s.started <- struct{}{}:
	default:
	}
}

type internalRegistryValue struct {
	Value int
}

func bareWebServer() *WebServer {
	return &WebServer{
		childServer:     make(map[int]*WebServer),
		serviceContexts: make(map[string]*router.ServiceContext),
		serverOption:    make(map[string]*types.ServerOption),
	}
}

func newConcurrencyTestContext(service *concurrencyTestService) *router.ServiceContext {
	serverConfig := &config.ServerConfig{
		AttachServices: make(map[string]*config.AttachAddress),
	}
	serverConfig.Name = service.name
	ctx := &router.ServiceContext{
		StateChan: make(chan bool, 1),
		Config:    serverConfig,
		Service: &types.Service{
			Name:     service.name,
			Instance: service,
		},
	}
	ctx.Router = router.NewServiceRouter(ctx, service)
	return ctx
}

func waitForStart(t *testing.T, started <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-started:
	case <-time.After(callbackTimeout):
		t.Fatalf("timed out waiting for %s start callback", name)
	}
}

func TestWebServerStartCallbackIsPerInstance(t *testing.T) {
	prefix := fmt.Sprintf("callback-%d", time.Now().UnixNano())

	firstStarted := make(chan struct{}, 1)
	firstService := &concurrencyTestService{name: prefix + "-first", started: firstStarted}
	first := bareWebServer()
	first.htmls = NewHTMLServer(0)
	firstContext := newConcurrencyTestContext(firstService)
	first.AddServiceContext(firstContext)
	firstContext.SetRunState(true)
	waitForStart(t, firstStarted, firstService.name)

	secondStarted := make(chan struct{}, 1)
	secondService := &concurrencyTestService{name: prefix + "-second", started: secondStarted}
	second := bareWebServer()
	second.htmls = NewHTMLServer(0)
	secondContext := newConcurrencyTestContext(secondService)
	second.AddServiceContext(secondContext)
	secondContext.SetRunState(true)
	waitForStart(t, secondStarted, secondService.name)
}

func TestStateCallbackUsesCommittedContextSnapshot(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previousProcs)

	prefix := fmt.Sprintf("committed-snapshot-%d", time.Now().UnixNano())
	readyStarted := make(chan struct{}, 1)
	readyService := &concurrencyTestService{name: prefix + "-ready", started: readyStarted}
	webServer := bareWebServer()
	htmlServer := NewHTMLServer(0)
	htmlServer.Isstart = make(chan bool)
	webServer.htmls = htmlServer
	readyContext := newConcurrencyTestContext(readyService)

	webServer.AddServiceContext(readyContext)
	readyContext.SetRunState(true)
	waitForWebServerRunState(t, webServer, true)

	lateStarted := make(chan struct{}, 1)
	lateService := &concurrencyTestService{name: prefix + "-late", started: lateStarted}
	lateContext := newConcurrencyTestContext(lateService)
	webServer.AddServiceContext(lateContext)
	lateSignal := true
	lateContext.StateChan <- lateSignal
	select {
	case got := <-lateContext.StateChan:
		if got != lateSignal {
			t.Fatalf("late StateChan value = %v, want %v", got, lateSignal)
		}
	case <-time.After(callbackTimeout):
		t.Fatal("late context StateChan was consumed after server was already running")
	}
	select {
	case <-htmlServer.Isstart:
	case <-time.After(callbackTimeout):
		t.Fatal("timed out releasing committed state callback")
	}
	waitForStart(t, readyStarted, readyService.name)
	runtime.Gosched()
	select {
	case <-lateStarted:
		t.Fatal("context added after state commit was included in the current start callback")
	default:
	}
}

func TestWebServerWithoutBeginDoesNotEndOtherInitialization(t *testing.T) {
	config.BeginServerInitialization()
	t.Cleanup(config.EndServerInitialization)

	bareWebServer().endInitialization()
	if !config.IsServerInitializing() {
		t.Fatal("WebServer without Begin ended another instance's initialization")
	}
}

func TestWebServerInitializationOnceTracksOverlappingInstances(t *testing.T) {
	first := bareWebServer()
	first.beginInitialization()
	t.Cleanup(first.endInitialization)
	second := bareWebServer()
	second.beginInitialization()
	t.Cleanup(second.endInitialization)

	first.endInitialization()
	if first.initializing.Load() {
		t.Fatal("first WebServer remained marked initializing after End")
	}
	if !config.IsServerInitializing() {
		t.Fatal("first WebServer End cleared the overlapping second instance")
	}

	second.endInitialization()
	if second.initializing.Load() {
		t.Fatal("second WebServer remained marked initializing after End")
	}
	if config.IsServerInitializing() {
		t.Fatal("initialization remained active after both instances ended")
	}
}

func waitForWebServerRunState(t *testing.T, webServer *WebServer, want bool) {
	t.Helper()
	deadline := time.NewTimer(callbackTimeout)
	defer deadline.Stop()
	for {
		webServer.RLock()
		got := webServer.isRun
		webServer.RUnlock()
		if got == want {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("timed out waiting for WebServer isRun=%v", want)
		default:
			runtime.Gosched()
		}
	}
}

func TestWebServerStateCallbackAllowsNilHTMLServer(t *testing.T) {
	name := fmt.Sprintf("nil-html-%d", time.Now().UnixNano())
	started := make(chan struct{}, 1)
	service := &concurrencyTestService{name: name, started: started}
	webServer := bareWebServer()
	ctx := newConcurrencyTestContext(service)
	webServer.AddServiceContext(ctx)
	ctx.SetRunState(true)
	waitForStart(t, started, name)
}

func TestLinkServiceMatchesAttachNameCaseInsensitively(t *testing.T) {
	prefix := fmt.Sprintf("attach-case-%d", time.Now().UnixNano())
	providerName := prefix + "-provider"
	providerService := &concurrencyTestService{name: providerName, started: make(chan struct{}, 1)}
	provider := newConcurrencyTestContext(providerService)
	provider.Config.RunIp = "127.0.0.42"
	provider.Config.Port = 18442
	provider.Config.SocketPort = 19442

	consumerName := prefix + "-consumer"
	consumerService := &concurrencyTestService{name: consumerName, started: make(chan struct{}, 1)}
	consumer := newConcurrencyTestContext(consumerService)
	attach := &config.AttachAddress{Name: strings.ToUpper(providerName)}
	consumer.Config.AttachServices[attach.Name] = attach

	webServer := bareWebServer()
	webServer.serviceContexts[strings.ToLower(providerName)] = provider
	webServer.serviceContexts[strings.ToLower(consumerName)] = consumer
	webServer.linkServiceContexts(webServer.serviceContextSnapshot())

	if attach.Address != provider.Config.RunIp || attach.Port != provider.Config.Port || attach.SocketPort != provider.Config.SocketPort {
		t.Fatalf("mixed-case attach was not resolved: %#v", attach)
	}
}

func TestGetServerOptionsReturnsDeepSnapshot(t *testing.T) {
	fileSystem := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("demo")}}
	webServer := bareWebServer()
	webServer.serverOption["orders"] = &types.ServerOption{
		IsCors:                true,
		OriginCors:            []string{"https://example.com"},
		WhiteList:             []string{"127.0.0.1"},
		RemoteAccessManageAPI: true,
		Demo:                  &types.DemoOption{Pattern: "demo", File: fileSystem},
		Trans:                 &types.TransOption{IsRest: true, RetryCount: 3},
		Quic:                  &types.QuicOption{IsQuic: true, CertFile: "cert.pem", KeyFile: "key.pem"},
	}

	snapshot := webServer.GetServerOptions()
	got := snapshot["orders"]
	delete(snapshot, "orders")
	snapshot["other"] = &types.ServerOption{}
	got.IsCors = false
	got.RemoteAccessManageAPI = false
	got.OriginCors[0] = "https://mutated.example.com"
	got.WhiteList[0] = "0.0.0.0"
	got.Demo.Pattern = "mutated"
	got.Trans.RetryCount = 99
	got.Quic.CertFile = "mutated.pem"

	internal := webServer.GetServerOption("orders")
	if internal == nil {
		t.Fatal("internal option was removed through returned map")
	}
	if webServer.GetServerOption("other") != nil {
		t.Fatal("returned map mutation added an option to internal state")
	}
	if !internal.IsCors || !internal.RemoteAccessManageAPI {
		t.Fatal("ordinary fields were mutated through returned option")
	}
	if internal.OriginCors[0] != "https://example.com" || internal.WhiteList[0] != "127.0.0.1" {
		t.Fatal("slice fields were mutated through returned option")
	}
	if internal.Demo.Pattern != "demo" || internal.Trans.RetryCount != 3 || internal.Quic.CertFile != "cert.pem" {
		t.Fatal("pointer fields were mutated through returned option")
	}
	if !sameFileSystem(internal.Demo.File, fileSystem) {
		t.Fatal("Demo.File interface reference should be preserved")
	}

	internal.Demo.Pattern = "mutated-again"
	if webServer.GetServerOption("orders").Demo.Pattern != "demo" {
		t.Fatal("GetServerOption exposed an internal pointer")
	}
}

func TestSetOptionStoresClone(t *testing.T) {
	name := fmt.Sprintf("option-%d", time.Now().UnixNano())
	service := &concurrencyTestService{name: name, started: make(chan struct{}, 1)}
	webServer := bareWebServer()
	webServer.serviceContexts[name] = newConcurrencyTestContext(service)
	original := &types.ServerOption{
		OriginCors: []string{"https://example.com"},
		Demo:       &types.DemoOption{Pattern: "demo"},
	}

	webServer.SetOption(service, original)
	original.OriginCors[0] = "https://mutated.example.com"
	original.Demo.Pattern = "mutated"

	stored := webServer.GetServerOption(name)
	if stored.OriginCors[0] != "https://example.com" || stored.Demo.Pattern != "demo" {
		t.Fatal("SetOption retained caller-owned option data")
	}
}

func TestWebServerOptionKeysAreCaseInsensitive(t *testing.T) {
	name := fmt.Sprintf("MiXeD-Option-%d", time.Now().UnixNano())
	service := &concurrencyTestService{name: name, started: make(chan struct{}, 1)}
	webServer := bareWebServer()
	ctx := newConcurrencyTestContext(service)
	webServer.AddServiceContext(ctx)

	option := &types.ServerOption{Trans: &types.TransOption{RetryCount: 7}}
	webServer.SetOption(service, option)

	lowerName := strings.ToLower(name)
	if _, ok := webServer.serviceContexts[lowerName]; !ok {
		t.Fatalf("AddServiceContext did not store context under lowercase key %q", lowerName)
	}
	if got := webServer.GetServerOption(strings.ToUpper(name)); got == nil || got.Trans.RetryCount != 7 {
		t.Fatalf("GetServerOption with mixed case returned %#v", got)
	}
	if got := ctx.GetServerOption(); got == nil || got.Trans.RetryCount != 7 {
		t.Fatalf("SetOption did not apply option to lowercase context: %#v", got)
	}
}

func TestConcurrentSetOptionKeepsContextConsistent(t *testing.T) {
	name := fmt.Sprintf("consistent-option-%d", time.Now().UnixNano())
	service := &concurrencyTestService{name: name, started: make(chan struct{}, 1)}
	webServer := bareWebServer()
	ctx := newConcurrencyTestContext(service)
	webServer.serviceContexts[strings.ToLower(name)] = ctx

	const (
		rounds  = 200
		workers = 24
	)
	for round := 0; round < rounds; round++ {
		start := make(chan struct{})
		var wait sync.WaitGroup
		wait.Add(workers)
		for worker := 0; worker < workers; worker++ {
			worker := worker
			go func() {
				defer wait.Done()
				<-start
				value := round*workers + worker
				webServer.SetOption(service, &types.ServerOption{
					Trans: &types.TransOption{RetryCount: value},
				})
			}()
		}
		close(start)
		wait.Wait()

		stored := webServer.GetServerOption(name)
		applied := ctx.GetServerOption()
		if stored == nil || applied == nil || stored.Trans == nil || applied.Trans == nil {
			t.Fatalf("round %d produced nil option: stored=%#v applied=%#v", round, stored, applied)
		}
		if stored.Trans.RetryCount != applied.Trans.RetryCount {
			t.Fatalf("round %d left inconsistent options: stored=%d applied=%d", round, stored.Trans.RetryCount, applied.Trans.RetryCount)
		}
	}
}

func TestWebServerConcurrentRegistryAccess(t *testing.T) {
	prefix := fmt.Sprintf("registry-%d", time.Now().UnixNano())
	webServer := bareWebServer()
	baseService := &concurrencyTestService{name: prefix + "-base", started: make(chan struct{}, 1)}
	webServer.serviceContexts[baseService.name] = newConcurrencyTestContext(baseService)

	const workers = 24
	done := make(chan struct{}, workers*3)
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			<-start
			service := &concurrencyTestService{
				name:    fmt.Sprintf("%s-added-%d", prefix, i),
				started: make(chan struct{}, 1),
			}
			ctx := newConcurrencyTestContext(service)
			ctx.StateChan <- false
			webServer.AddServiceContext(ctx)
			done <- struct{}{}
		}()
		go func() {
			<-start
			webServer.SetOption(baseService, &types.ServerOption{
				OriginCors: []string{fmt.Sprintf("https://%d.example.com", i)},
			})
			done <- struct{}{}
		}()
		go func() {
			<-start
			_ = webServer.GetServerOptions()
			_ = webServer.GetServerOption(baseService.name)
			done <- struct{}{}
		}()
	}
	close(start)
	for i := 0; i < workers*3; i++ {
		select {
		case <-done:
		case <-time.After(callbackTimeout):
			t.Fatal("timed out waiting for concurrent registry access")
		}
	}
}

func TestInternalServiceRegistryConcurrentAccess(t *testing.T) {
	prefix := fmt.Sprintf("internal-%d", time.Now().UnixNano())
	const workers = 32
	done := make(chan struct{}, workers)
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		i := i
		go func() {
			<-start
			key := fmt.Sprintf("%s-%d", prefix, i)
			value := &internalRegistryValue{Value: i}
			if err := SetInternalService(key, value); err != nil {
				t.Errorf("SetInternalService(%q): %v", key, err)
				done <- struct{}{}
				return
			}
			if got := GetInternalService[internalRegistryValue](key); got == nil || got.Value != i {
				t.Errorf("GetInternalService(%q) = %#v, want Value %d", key, got, i)
			}
			done <- struct{}{}
		}()
	}
	close(start)
	for i := 0; i < workers; i++ {
		select {
		case <-done:
		case <-time.After(callbackTimeout):
			t.Fatal("timed out waiting for internal registry access")
		}
	}
}

func TestGetInternalServiceReturnsNilForWrongStoredType(t *testing.T) {
	key := fmt.Sprintf("wrong-type-%d", time.Now().UnixNano())
	typeName := utils.GetTypeName(new(internalRegistryValue))
	typemapMu.Lock()
	if typemap[typeName] == nil {
		typemap[typeName] = make(map[string]interface{})
	}
	typemap[typeName][key] = "not an internalRegistryValue"
	typemapMu.Unlock()
	t.Cleanup(func() {
		typemapMu.Lock()
		defer typemapMu.Unlock()
		delete(typemap[typeName], key)
		if len(typemap[typeName]) == 0 {
			delete(typemap, typeName)
		}
	})

	if got := GetInternalService[internalRegistryValue](key); got != nil {
		t.Fatalf("GetInternalService returned %#v for a mismatched stored type", got)
	}
}

func sameFileSystem(left, right fs.FS) bool {
	leftFile, leftErr := left.Open("index.html")
	rightFile, rightErr := right.Open("index.html")
	if leftErr != nil || rightErr != nil {
		return false
	}
	leftFile.Close()
	rightFile.Close()
	return true
}
