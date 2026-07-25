package run

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/digitalwayhk/core/pkg/server"
	"github.com/digitalwayhk/core/pkg/server/api/release"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"

	"github.com/digitalwayhk/core/pkg/server/trans/rest"
	grpctransport "github.com/digitalwayhk/core/pkg/server/transport/grpc"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/proc"
	"github.com/zeromicro/go-zero/core/service"
)

type WebServer struct {
	sync.RWMutex
	serviceContexts            map[string]*router.ServiceContext
	serverOption               map[string]*types.ServerOption
	childServer                map[int]*WebServer
	htmls                      *HTMLServer
	manageAuthAuthorityService string
	ViewPort                   int
	Port                       int
	GRPCPort                   int
	isRun                      bool
	registryVersion            uint64
	optionApplyMu              sync.Mutex
	initOnce                   sync.Once
	endOnce                    sync.Once
	initializing               atomic.Bool
	startOnce                  sync.Once
	runMu                      sync.Mutex
	runOnce                    sync.Once
	runLifecycleOnce           sync.Once
	shutdownOnce               sync.Once
	runReadyOnce               sync.Once
	runDoneOnce                sync.Once
	stopCloseOnce              sync.Once
	runStarted                 atomic.Bool
	stopped                    atomic.Bool
	runReady                   chan struct{}
	runDone                    chan struct{}
	stopCh                     chan struct{}
	group                      *service.ServiceGroup
	saveConfig                 func(*config.ServerConfig) error
}

func (own *WebServer) persistConfig(cfg *config.ServerConfig) error {
	if own.saveConfig != nil {
		return own.saveConfig(cfg)
	}
	return cfg.Save()
}

var _ sync.Locker = (*WebServer)(nil)

func (own *WebServer) GetServerOptions() map[string]*types.ServerOption {
	own.RLock()
	defer own.RUnlock()

	options := make(map[string]*types.ServerOption, len(own.serverOption))
	for name, option := range own.serverOption {
		options[name] = option.Clone()
	}
	return options
}
func (own *WebServer) GetServerOption(name string) *types.ServerOption {
	own.RLock()
	defer own.RUnlock()
	return own.serverOption[strings.ToLower(name)].Clone()
}

// SetManageAuthAuthority 选择 ViewPort 使用的 Manage Auth 权威服务。
// 该进程级关系只能在 Start 前设置，不写入任一服务配置。
func (own *WebServer) SetManageAuthAuthority(serviceName string) error {
	own.runMu.Lock()
	defer own.runMu.Unlock()
	if own.runStarted.Load() {
		return errors.New("Manage Auth 权威只能在启动前配置")
	}
	own.Lock()
	defer own.Unlock()
	own.manageAuthAuthorityService = normalizeServiceName(serviceName)
	return nil
}

func (own *WebServer) manageAuthAuthoritySnapshot() string {
	own.RLock()
	defer own.RUnlock()
	return own.manageAuthAuthorityService
}

func NewWebServer() *WebServer {
	ws := &WebServer{
		childServer:     make(map[int]*WebServer),
		serviceContexts: make(map[string]*router.ServiceContext),
		serverOption:    make(map[string]*types.ServerOption),
	}
	ws.beginInitialization()
	ws.AddIService(&server.SystemManage{})
	return ws
}
func (own *WebServer) AddServiceContext(sc *router.ServiceContext) {
	sc.Router.AddServerRouters(release.Routers()...)
	name := strings.ToLower(sc.Service.Name)
	own.Lock()
	if own.serviceContexts == nil {
		own.serviceContexts = make(map[string]*router.ServiceContext)
	}
	own.serviceContexts[name] = sc
	own.registryVersion++
	alreadyRunning := own.isRun
	own.Unlock()
	if !alreadyRunning {
		go own.stateCallback(sc)
	}
	go own.failureCallback(sc)
}

func (own *WebServer) failureCallback(sc *router.ServiceContext) {
	select {
	case err := <-sc.Failure():
		logx.Errorw("service_runtime_failed",
			logx.Field("service", sc.Service.Name),
			logx.Field("error", err),
		)
		own.Stop()
	case <-own.stopChannel():
	}
}

func (own *WebServer) stateCallback(nsc *router.ServiceContext) {
	select {
	case <-nsc.StateChan:
	case <-own.stopChannel():
		return
	}

	for {
		contexts, version := own.serviceContextSnapshotWithVersion()
		allRunning := true
		for _, ctx := range contexts {
			if !ctx.IsRun() {
				allRunning = false
				break
			}
		}
		if !allRunning {
			return
		}

		own.Lock()
		if own.isRun {
			own.Unlock()
			return
		}
		if own.registryVersion != version {
			own.Unlock()
			continue
		}
		own.isRun = true
		htmls := own.htmls
		viewPort := own.ViewPort
		own.Unlock()

		own.startOnce.Do(func() {
			if htmls != nil {
				htmls.Isstart <- viewPort > 0
			}
			own.endInitialization()
			own.serviceStartContexts(contexts)
		})
		return
	}
}

func (own *WebServer) serviceStartContexts(contexts []*router.ServiceContext) {
	for _, ctx := range contexts {
		if start, ok := ctx.Service.Instance.(types.IStartService); ok {
			logx.Infow("service_hook_starting", logx.Field("service", ctx.Service.Name))
			go start.Start()
		}
	}
}
func (own *WebServer) AddIService(service types.IService, option ...*types.ServerOption) {
	sc := router.NewServiceContext(service)
	own.AddServiceContext(sc)
	if len(option) > 0 {
		own.setOption(strings.ToLower(service.ServiceName()), option[0])
	}
}
func (own *WebServer) SetOption(service types.IService, option *types.ServerOption) {
	own.setOption(strings.ToLower(service.ServiceName()), option)
}

func (own *WebServer) setOption(name string, option *types.ServerOption) {
	name = strings.ToLower(name)
	stored := option.Clone()
	own.optionApplyMu.Lock()
	defer own.optionApplyMu.Unlock()

	own.Lock()
	if own.serverOption == nil {
		own.serverOption = make(map[string]*types.ServerOption)
	}
	own.serverOption[name] = stored
	ctx := own.serviceContexts[name]
	own.Unlock()

	if ctx != nil {
		ctx.SetServerOption(stored)
	}
}

func (own *WebServer) beginInitialization() {
	own.initOnce.Do(func() {
		config.BeginServerInitialization()
		own.initializing.Store(true)
	})
}

func (own *WebServer) endInitialization() {
	if !own.initializing.Load() {
		return
	}
	own.endOnce.Do(func() {
		config.EndServerInitialization()
		own.initializing.Store(false)
	})
}

func (own *WebServer) serviceContextSnapshot() []*router.ServiceContext {
	contexts, _ := own.serviceContextSnapshotWithVersion()
	return contexts
}

func (own *WebServer) serviceContextSnapshotWithVersion() ([]*router.ServiceContext, uint64) {
	own.RLock()
	defer own.RUnlock()

	contexts := make([]*router.ServiceContext, 0, len(own.serviceContexts))
	for _, ctx := range own.serviceContexts {
		contexts = append(contexts, ctx)
	}
	return contexts, own.registryVersion
}

func (own *WebServer) htmlServerSnapshot() *HTMLServer {
	own.RLock()
	defer own.RUnlock()
	return own.htmls
}

func (own *WebServer) Start() {
	own.prepareRunLifecycle()
	own.runOnce.Do(func() {
		var constructed []service.Service
		defer func() {
			if failure := recover(); failure != nil {
				for index := len(constructed) - 1; index >= 0; index-- {
					constructed[index].Stop()
				}
				own.endInitialization()
				own.stopped.Store(true)
				own.closeStopChannel()
				own.markRunReady()
				own.markRunDone()
				panic(failure)
			}
		}()
		own.runMu.Lock()
		if own.stopped.Load() {
			own.runMu.Unlock()
			return
		}
		own.runStarted.Store(true)
		own.runMu.Unlock()
		own.beginInitialization()
		var err error
		constructed, err = own.initServer()
		if err != nil {
			panic(err)
		}
		group := service.NewServiceGroup()
		for _, server := range constructed {
			group.Add(server)
		}
		//todo:test quic server
		// for _, ctx := range own.serviceContexts {
		// 	group.Add(quic.NewServer(ctx))
		// }
		group.Add(own.htmlServerSnapshot())
		own.runServiceGroup(group)
	})
}

func (own *WebServer) Stop() {
	own.prepareRunLifecycle()
	own.runMu.Lock()
	own.stopped.Store(true)
	started := own.runStarted.Load()
	own.runMu.Unlock()
	own.shutdownOnce.Do(func() {
		own.closeStopChannel()
		if !started {
			own.markRunReady()
			own.markRunDone()
			return
		}
		own.RLock()
		group := own.group
		own.RUnlock()
		if group == nil {
			select {
			case <-own.runReady:
			case <-own.runDone:
				return
			}
			own.RLock()
			group = own.group
			own.RUnlock()
		}
		if group != nil {
			proc.Shutdown()
			group.Stop()
		}
	})
	if started {
		<-own.runDone
	}
}

func (own *WebServer) closeStopChannel() {
	own.stopCloseOnce.Do(func() { close(own.stopCh) })
}

func (own *WebServer) markRunReady() {
	own.runReadyOnce.Do(func() { close(own.runReady) })
}

func (own *WebServer) markRunDone() {
	own.runDoneOnce.Do(func() { close(own.runDone) })
}

func (own *WebServer) prepareRunLifecycle() {
	own.runLifecycleOnce.Do(func() {
		own.runReady = make(chan struct{})
		own.runDone = make(chan struct{})
		own.stopCh = make(chan struct{})
	})
}

func (own *WebServer) stopChannel() <-chan struct{} {
	own.prepareRunLifecycle()
	return own.stopCh
}

func (own *WebServer) runServiceGroup(group *service.ServiceGroup) {
	defer own.markRunDone()
	defer func() {
		group.Stop()
		for _, ctx := range own.serviceContextSnapshot() {
			if stop, ok := ctx.Service.Instance.(types.IStopService); ok {
				stop.Stop()
			}
		}
	}()

	group.Add(service.WithStart(own.markRunReady))
	own.Lock()
	own.group = group
	own.Unlock()
	group.Start()
}

func (own *WebServer) initServer() ([]service.Service, error) {
	own.serverArgs()
	return own.initializeServers(own.serviceContextSnapshot())
}

func (own *WebServer) initializeServers(contexts []*router.ServiceContext) ([]service.Service, error) {
	for _, ctx := range contexts {
		if ctx.Config.Port != own.Port && own.Port != router.DEFAULTPORT {
			ctx.Config.Port = own.Port + int(ctx.Config.DataCenterID) - 1
		}
	}
	ordered, err := precomputeServicePorts(contexts, own.GRPCPort)
	if err != nil {
		return nil, fmt.Errorf("初始化服务端口失败：%w", err)
	}
	for _, ctx := range ordered {
		if err := ctx.Config.Validate(); err != nil {
			return nil, fmt.Errorf("初始化服务配置失败，服务名称：%s，错误信息：%w", ctx.Config.Name, err)
		}
	}
	authority, err := resolveManageAuthAuthority(ordered, own.manageAuthAuthoritySnapshot())
	if err != nil {
		return nil, fmt.Errorf("初始化 Manage Auth 权威失败：%w", err)
	}
	htmls := NewHTMLServer(own.ViewPort)
	htmls.Parent = own
	htmls.SetManageAuthAuthority(authority)
	own.Lock()
	own.htmls = htmls
	own.Unlock()
	constructed := make([]service.Service, 0, len(ordered)*2)
	rollback := func() {
		for index := len(constructed) - 1; index >= 0; index-- {
			constructed[index].Stop()
		}
	}
	for _, ctx := range ordered {
		if err := own.newWebServer(ctx); err != nil {
			rollback()
			return nil, fmt.Errorf("初始化 HTTP 服务失败，服务名称：%s，错误信息：%w", ctx.Config.Name, err)
		}
		constructed = append(constructed, ctx.Service.HttpServer)
		if err := own.newInternalServer(ctx); err != nil {
			rollback()
			return nil, fmt.Errorf("初始化 gRPC 服务失败，服务名称：%s，地址：%s:%d，错误信息：%w",
				ctx.Config.Name, ctx.Config.Host, ctx.Config.Transport.GRPC.Port, err)
		}
		servers := ctx.GetServers()
		if len(servers) > 1 {
			constructed = append(constructed, servers[1:]...)
		}
		if err := own.persistConfig(ctx.Config); err != nil {
			rollback()
			return nil, fmt.Errorf("初始化服务器异常，服务名称：%s，错误信息：%w", ctx.Config.Name, err)
		}
		htmls.AddServiceRouter(ctx.Router)
	}
	if err := htmls.Prepare(); err != nil {
		rollback()
		own.Lock()
		if own.htmls == htmls {
			own.htmls = nil
		}
		own.Unlock()
		return nil, fmt.Errorf("初始化 HTML 服务失败：%w", err)
	}
	return constructed, nil
}
func (own *WebServer) serverArgs() {
	port := flag.Int("p", router.DEFAULTPORT, "运行端口,默认8080")
	grpcPort := flag.Int("grpc", 0, "覆盖gRPC服务端口,为0时使用各服务配置")
	view := flag.Int("view", 80, "启用视图服务并指定端口,为0时不启用视图服务")
	flag.Parse()
	if own.ViewPort == 0 {
		own.ViewPort = *view
	}
	if own.Port == 0 {
		own.Port = *port
	}
	if own.GRPCPort == 0 {
		own.GRPCPort = *grpcPort
	}
}
func (own *WebServer) newWebServer(ctx *router.ServiceContext) error {
	var rs *rest.Server
	var err error
	if opt := own.GetServerOption(ctx.Service.Name); opt != nil {
		rs, err = rest.NewServer(ctx, opt.IsWebSocket, opt.IsCors, opt.OriginCors...)
	} else {
		rs, err = rest.NewServer(ctx, false, false)
	}
	if err != nil {
		return err
	}
	ctx.SetHttpServer(rs)
	return nil
}
func (own *WebServer) newInternalServer(ctx *router.ServiceContext) error {
	grpcConfig := ctx.Config.Transport.GRPC
	address := net.JoinHostPort(ctx.Config.Host, strconv.Itoa(grpcConfig.Port))
	server, err := grpctransport.NewServer(address, grpcConfig, ctx.HandleInternalPayload)
	if err != nil {
		return err
	}
	_, portText, err := net.SplitHostPort(server.Address())
	if err != nil {
		server.Stop()
		return fmt.Errorf("parse bound address %q: %w", server.Address(), err)
	}
	boundPort, err := strconv.Atoi(portText)
	if err != nil {
		server.Stop()
		return fmt.Errorf("parse bound port %q: %w", portText, err)
	}
	ctx.Config.Transport.GRPC.Port = boundPort
	ctx.SetGRPCServer(server)

	return nil
}

func grpcPortOverride(base, index int) (int, error) {
	if base == 0 {
		return 0, nil
	}
	if index < 0 {
		return 0, fmt.Errorf("gRPC port index must be non-negative, got %d", index)
	}
	port := base + index
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("gRPC port override %d with index %d is outside 1..65535", base, index)
	}
	return port, nil
}

func precomputeServicePorts(contexts []*router.ServiceContext, grpcBase int) ([]*router.ServiceContext, error) {
	ordered := append([]*router.ServiceContext(nil), contexts...)
	sort.Slice(ordered, func(i, j int) bool {
		return strings.ToLower(ordered[i].Service.Name) < strings.ToLower(ordered[j].Service.Name)
	})
	used := make(map[int]string)
	plannedGRPC := make(map[*router.ServiceContext]int, len(ordered))
	reserve := func(port int, protocol, serviceName string) error {
		if port == 0 {
			return nil
		}
		if port < 1 || port > 65535 {
			return fmt.Errorf("%s port %d for service %s is outside 1..65535", protocol, port, serviceName)
		}
		if previous, ok := used[port]; ok {
			return fmt.Errorf("duplicate %s port %d for service %s conflicts with %s", protocol, port, serviceName, previous)
		}
		used[port] = serviceName + " " + protocol
		return nil
	}
	for index, ctx := range ordered {
		grpcPort, err := grpcPortOverride(grpcBase, index)
		if err != nil {
			return nil, err
		}
		if grpcPort != 0 {
			plannedGRPC[ctx] = grpcPort
		} else {
			plannedGRPC[ctx] = ctx.Config.Transport.GRPC.Port
		}
		serviceName := ctx.Service.Name
		if err := reserve(ctx.Config.Port, "HTTP", serviceName); err != nil {
			return nil, err
		}
		if err := reserve(plannedGRPC[ctx], "gRPC", serviceName); err != nil {
			return nil, err
		}
	}
	for ctx, port := range plannedGRPC {
		ctx.Config.Transport.GRPC.Port = port
	}
	return ordered, nil
}

var (
	typemap   = make(map[string]map[string]interface{})
	typemapMu sync.RWMutex
)

func SetInternalService[T any](key string, service *T) error {
	if service == nil {
		return errors.New("service is nil")
	}
	name := utils.GetTypeName(service)
	typemapMu.Lock()
	defer typemapMu.Unlock()
	if _, ok := typemap[name]; !ok {
		typemap[name] = make(map[string]interface{})
	}
	typemap[name][key] = service
	return nil
}
func GetInternalService[T any](key string) *T {
	name := utils.GetTypeName(new(T))
	typemapMu.RLock()
	defer typemapMu.RUnlock()
	if _, ok := typemap[name]; ok {
		if v, ok := typemap[name][key]; ok {
			service, ok := v.(*T)
			if !ok {
				return nil
			}
			return service
		}
	}
	return nil
}
