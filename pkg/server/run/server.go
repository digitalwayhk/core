package run

import (
	"errors"
	"flag"
	"fmt"
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

	"github.com/zeromicro/go-zero/core/service"
)

type WebServer struct {
	sync.RWMutex
	serviceContexts map[string]*router.ServiceContext
	serverOption    map[string]*types.ServerOption
	childServer     map[int]*WebServer
	htmls           *HTMLServer
	ViewPort        int
	serverip        string
	Port            int
	SocketPort      int
	isRun           bool
	registryVersion uint64
	optionApplyMu   sync.Mutex
	initOnce        sync.Once
	endOnce         sync.Once
	initializing    atomic.Bool
	startOnce       sync.Once
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
}

func (own *WebServer) stateCallback(nsc *router.ServiceContext) {
	<-nsc.StateChan

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
			fmt.Println("===========================================================")
			fmt.Println("服务" + ctx.Service.Name + "的IStartService接口开始执行")
			go start.Start()
			fmt.Println("===========================================================")
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
	fmt.Println("===========================================================")
	fmt.Println("全部服务启动成功，开始连接依赖服务。。。")
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
					msg := ctx.Service.Name + "服务中连接" + cfg.Name + "服务,地址:" + cfg.Address + ":" + strconv.Itoa(cfg.Port) + "异常，异常信息：" + err.Error()
					fmt.Println(msg)
				} else {
					msg := ctx.Service.Name + "服务中连接" + cfg.Name + "服务,地址:" + cfg.Address + ":" + strconv.Itoa(cfg.Port) + "成功"
					fmt.Println(msg)
				}
			} else {
				msg := cfg.Name + "服务待连接,但未设置地址和端口，请设置地址的端口号"
				fmt.Println(msg)
			}
		}
	}
	fmt.Println("===========================================================")
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
	own.beginInitialization()
	own.initServer()
	group := service.NewServiceGroup()
	defer func() {
		group.Stop()
		for _, ctx := range own.serviceContextSnapshot() {
			if stop, ok := ctx.Service.Instance.(types.IStopService); ok {
				go stop.Stop()
			}
		}
	}()
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
		err := ctx.Config.Save()
		if err != nil {
			msg := "初始化服务器异常，服务名称：" + ctx.Config.Name + "，错误信息：" + err.Error()
			panic(msg)
		}
		if err := own.newWebServer(ctx); err != nil {
			panic(fmt.Sprintf("初始化 HTTP 服务失败，服务名称：%s，错误信息：%v", ctx.Config.Name, err))
		}
		own.newInternalServer(ctx)
		htmls.AddServiceRouter(ctx.Router)
	}
}
func (own *WebServer) serverArgs() {
	parentServer := flag.String("server", "", "主服务器地址,当前服务器的父服务器地址,如果是根服务器，则不需要此参数")
	port := flag.Int("p", router.DEFAULTPORT, "运行端口,默认8080")
	socket := flag.Int("socket", router.DEFAULTSOCKETPORT, "启用Socket服务并指定端口,为0时不启用Socket服务")
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
func (own *WebServer) newInternalServer(ctx *router.ServiceContext) {
	if own.SocketPort > 0 {
		ss := socket.NewServer(ctx)
		ctx.SetSocketServer(ss)
	}
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
