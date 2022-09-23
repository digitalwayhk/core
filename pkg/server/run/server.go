package run

import (
	"flag"
	"fmt"
	"strconv"
	"sync"

	"github.com/digitalwayhk/core/pkg/server"
	"github.com/digitalwayhk/core/pkg/server/api/public"
	"github.com/digitalwayhk/core/pkg/server/api/release"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"

	"github.com/digitalwayhk/core/pkg/server/trans/rest"
	"github.com/digitalwayhk/core/pkg/server/trans/socket"

	"github.com/zeromicro/go-zero/core/service"
)

type WebServer struct {
	serviceContexts map[string]*router.ServiceContext
	childServer     map[int]*WebServer
	htmls           *HTMLServer
	viewport        int
	serverip        string
	port            int
	socketport      int
	isRun           bool
	sync.Mutex
}

func NewWebServer() *WebServer {
	config.INITSERVER = true
	ws := &WebServer{
		childServer:     make(map[int]*WebServer),
		serviceContexts: make(map[string]*router.ServiceContext),
	}
	ws.AddIService(&server.SystemManage{})
	return ws
}
func (own *WebServer) AddServiceContext(sc *router.ServiceContext) {
	sc.Router.AddServerRouters(release.Routers()...)
	own.serviceContexts[sc.Service.Name] = sc
	go own.stateCallback(sc)
}
func (own *WebServer) stateCallback(nsc *router.ServiceContext) {
	if own.isRun {
		return
	}
	<-nsc.StateChan
	defer own.Unlock()
	own.Lock()
	for _, ctx := range own.serviceContexts {
		if !ctx.IsRun() {
			return
		}
	}
	own.isRun = true
	own.linkService()
	own.serviceStart()
}

func (own *WebServer) serviceStart() {
	for _, ctx := range own.serviceContexts {
		if start, ok := ctx.Service.Instance.(types.IStartService); ok {
			fmt.Println("===========================================================")
			fmt.Println("服务" + ctx.Service.Name + "的IStartService接口开始执行")
			start.Start()
			fmt.Println("===========================================================")
		}
	}
}
func (own *WebServer) linkService() {
	defer func() {
		config.INITSERVER = false
	}()
	fmt.Println("===========================================================")
	fmt.Println("全部服务启动成功，开始连接依赖服务。。。")
	for _, ctx := range own.serviceContexts {
		for _, cfg := range ctx.Config.AttachServices {
			if cfg.Address == "" && cfg.Port == 0 {
				context := own.serviceContexts[cfg.Name]
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

func (own *WebServer) AddIService(service types.IService) {
	sc := router.NewServiceContext(service)
	own.AddServiceContext(sc)
}

func (own *WebServer) Start() {
	config.INITSERVER = true
	own.initServer()
	group := service.NewServiceGroup()
	defer func() {
		group.Stop()
		for _, ctx := range own.serviceContexts {
			if stop, ok := ctx.Service.Instance.(types.IStopService); ok {
				go stop.Stop()
			}
		}
	}()
	for _, ctx := range own.serviceContexts {
		for _, server := range ctx.GetServers() {
			if server != nil {
				group.Add(server)
			}
		}
	}
	group.Add(own.htmls)
	group.Start()
}

func (own *WebServer) initServer() {
	own.serverArgs()
	own.htmls = NewHTMLServer(own.viewport)
	for _, ctx := range own.serviceContexts {
		if ctx.Config.ParentServerIP != own.serverip {
			ctx.Config.ParentServerIP = own.serverip
		}
		if ctx.Config.Port != own.port && own.port != router.DEFAULTPORT {
			ctx.Config.Port = own.port + int(ctx.Config.DataCenterID) - 1
		}
		if ctx.Config.SocketPort != own.socketport && own.socketport != router.DEFAULTSOCKETPORT {
			ctx.Config.SocketPort = own.socketport + int(ctx.Config.DataCenterID) - 1
		}
		err := ctx.Config.Save()
		if err != nil {
			msg := "初始化服务器异常，服务名称：" + ctx.Config.Name + "，错误信息：" + err.Error()
			panic(msg)
		}
		own.newWebServer(ctx)
		own.newInternalServer(ctx)
		own.htmls.AddServiceRouter(ctx.Router)
	}
}
func (own *WebServer) serverArgs() {
	parentServer := flag.String("server", "", "主服务器地址,当前服务器的父服务器地址,如果是根服务器，则不需要此参数")
	port := flag.Int("p", router.DEFAULTPORT, "运行端口,默认8080")
	socket := flag.Int("socket", router.DEFAULTSOCKETPORT, "启用Socket服务并指定端口,为0时不启用Socket服务")
	view := flag.Int("view", 80, "启用视图服务并指定端口,为0时不启用视图服务")
	flag.Parse()
	own.viewport = *view
	own.serverip = *parentServer
	own.port = *port
	own.socketport = *socket
}
func (own *WebServer) newWebServer(ctx *router.ServiceContext) {
	rs := rest.NewServer(ctx)
	ctx.SetHttpServer(rs)
}
func (own *WebServer) newInternalServer(ctx *router.ServiceContext) {
	if own.socketport > 0 {
		ss := socket.NewServer(ctx)
		ctx.SetSocketServer(ss)
	}
}
