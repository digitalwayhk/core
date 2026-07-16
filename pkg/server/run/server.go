package run

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/digitalwayhk/core/pkg/server"
	"github.com/digitalwayhk/core/pkg/server/api/public"
	"github.com/digitalwayhk/core/pkg/server/api/release"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"

	"github.com/digitalwayhk/core/pkg/server/trans/rest"
	"github.com/digitalwayhk/core/pkg/server/trans/socket"
	grpctransport "github.com/digitalwayhk/core/pkg/server/transport/grpc"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/proc"
	"github.com/zeromicro/go-zero/core/service"
)

type WebServer struct {
	sync.RWMutex
	serviceContexts  map[string]*router.ServiceContext
	serverOption     map[string]*types.ServerOption
	childServer      map[int]*WebServer
	htmls            *HTMLServer
	ViewPort         int
	serverip         string
	Port             int
	SocketPort       int
	GRPCPort         int
	isRun            bool
	registryVersion  uint64
	optionApplyMu    sync.Mutex
	initOnce         sync.Once
	endOnce          sync.Once
	initializing     atomic.Bool
	startOnce        sync.Once
	runMu            sync.Mutex
	runOnce          sync.Once
	runLifecycleOnce sync.Once
	shutdownOnce     sync.Once
	runStarted       atomic.Bool
	stopped          atomic.Bool
	runReady         chan struct{}
	runDone          chan struct{}
	stopCh           chan struct{}
	group            *service.ServiceGroup
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
			own.linkServiceContexts(contexts)
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
func (own *WebServer) linkServiceContexts(contexts []*router.ServiceContext) {
	defer own.endInitialization()
	contextsByName := make(map[string]*router.ServiceContext, len(contexts))
	for _, ctx := range contexts {
		contextsByName[strings.ToLower(ctx.Service.Name)] = ctx
	}
	islink := false
	for _, ctx := range contexts {
		if len(ctx.Config.AttachServices) > 0 {
			islink = true
			break
		}
	}
	if !islink {
		return
	}
	logx.Infow("service_dependencies_linking", logx.Field("service_count", len(contexts)))
	for _, ctx := range contexts {
		for _, cfg := range ctx.Config.AttachServices {
			if cfg.Address == "" && cfg.Port == 0 {
				context := contextsByName[strings.ToLower(cfg.Name)]
				if context != nil {
					cfg.Address = context.Config.RunIp
					cfg.Port = context.Config.Port
					cfg.SocketPort = context.Config.SocketPort
				}
				ctx.Config.Save()
			}
			if cfg.Address != "" && cfg.Port != 0 {
				ctx.SetAttachServiceAddress(cfg.Name)
				err := ctx.RegisterObserve(&public.Observe{})
				if err != nil {
					logx.Errorw("service_dependency_link_failed",
						logx.Field("service", ctx.Service.Name),
						logx.Field("dependency", cfg.Name),
						logx.Field("error", err),
					)
				} else {
					logx.Infow("service_dependency_linked",
						logx.Field("service", ctx.Service.Name),
						logx.Field("dependency", cfg.Name),
					)
				}
			} else {
				logx.Errorw("service_dependency_address_missing",
					logx.Field("service", ctx.Service.Name),
					logx.Field("dependency", cfg.Name),
				)
			}
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
		own.runMu.Lock()
		if own.stopped.Load() {
			own.runMu.Unlock()
			return
		}
		own.runStarted.Store(true)
		own.runMu.Unlock()
		own.beginInitialization()
		own.initServer()
		group := service.NewServiceGroup()
		for _, ctx := range own.serviceContextSnapshot() {
			for _, server := range ctx.GetServers() {
				if server != nil {
					group.Add(server)
				}
			}
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
		close(own.stopCh)
		if !started {
			return
		}
		select {
		case <-own.runReady:
			// go-zero v1.10.2 的 StartWithOpts 在 listener 关闭后仍等待进程级 shutdown listener。
			// WebServer 是应用级 owner，只在顶层 Stop 中统一触发，避免子服务递归关闭。
			proc.Shutdown()
		case <-own.runDone:
		}
	})
	if started {
		<-own.runDone
	}
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
	defer close(own.runDone)
	defer func() {
		group.Stop()
		for _, ctx := range own.serviceContextSnapshot() {
			if stop, ok := ctx.Service.Instance.(types.IStopService); ok {
				stop.Stop()
			}
		}
	}()

	group.Add(service.WithStart(func() { close(own.runReady) }))
	own.Lock()
	own.group = group
	own.Unlock()
	group.Start()
}

func (own *WebServer) initServer() {
	own.serverArgs()
	htmls := NewHTMLServer(own.ViewPort)
	htmls.Parent = own
	own.Lock()
	own.htmls = htmls
	own.Unlock()
	for _, ctx := range own.serviceContextSnapshot() {
		if ctx.Config.ParentServerIP != own.serverip {
			ctx.Config.ParentServerIP = own.serverip
		}
		if ctx.Config.Port != own.Port && own.Port != router.DEFAULTPORT {
			ctx.Config.Port = own.Port + int(ctx.Config.DataCenterID) - 1
		}
		if ctx.Config.SocketPort != own.SocketPort && own.SocketPort != router.DEFAULTSOCKETPORT {
			ctx.Config.SocketPort = own.SocketPort + int(ctx.Config.DataCenterID) - 1
		}
		grpcPort, err := grpcPortOverride(own.GRPCPort, ctx.Config.DataCenterID)
		if err != nil {
			panic(fmt.Sprintf("初始化 gRPC 端口失败，服务名称：%s，错误信息：%v", ctx.Config.Name, err))
		}
		if grpcPort != 0 {
			ctx.Config.Transport.GRPC.Port = grpcPort
		}
		if err := own.newWebServer(ctx); err != nil {
			panic(fmt.Sprintf("初始化 HTTP 服务失败，服务名称：%s，错误信息：%v", ctx.Config.Name, err))
		}
		if err := own.newInternalServer(ctx); err != nil {
			panic(fmt.Sprintf("初始化 gRPC 服务失败，服务名称：%s，地址：%s:%d，错误信息：%v",
				ctx.Config.Name, ctx.Config.Host, ctx.Config.Transport.GRPC.Port, err))
		}
		if err := ctx.Config.Save(); err != nil {
			panic("初始化服务器异常，服务名称：" + ctx.Config.Name + "，错误信息：" + err.Error())
		}
		htmls.AddServiceRouter(ctx.Router)
	}
}
func (own *WebServer) serverArgs() {
	parentServer := flag.String("server", "", "主服务器地址,当前服务器的父服务器地址,如果是根服务器，则不需要此参数")
	port := flag.Int("p", router.DEFAULTPORT, "运行端口,默认8080")
	socket := flag.Int("socket", router.DEFAULTSOCKETPORT, "启用Socket服务并指定端口,为0时不启用Socket服务")
	grpcPort := flag.Int("grpc", 0, "覆盖gRPC服务端口,为0时使用各服务配置")
	view := flag.Int("view", 80, "启用视图服务并指定端口,为0时不启用视图服务")
	flag.Parse()
	if own.ViewPort == 0 {
		own.ViewPort = *view
	}
	own.serverip = *parentServer
	if own.Port == 0 {
		own.Port = *port
	}
	if own.SocketPort == 0 {
		own.SocketPort = *socket
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

	if own.SocketPort > 0 && transportUsesProtocol(ctx.Config.Transport, "socket") {
		ss := socket.NewServer(ctx)
		ctx.SetSocketServer(ss)
	}
	return nil
}

func grpcPortOverride(base int, dataCenterID uint) (int, error) {
	if base == 0 {
		return 0, nil
	}
	offset := int(dataCenterID)
	if offset < 1 {
		offset = 1
	}
	port := base + offset - 1
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("gRPC port override %d with data center %d is outside 1..65535", base, dataCenterID)
	}
	return port, nil
}

func transportUsesProtocol(cfg config.TransportConfig, protocol string) bool {
	if cfg.Internal == protocol {
		return true
	}
	for _, fallback := range cfg.Fallback {
		if fallback == protocol {
			return true
		}
	}
	return false
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
