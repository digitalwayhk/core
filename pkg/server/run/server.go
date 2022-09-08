package run

import (
	"flag"
	"fmt"
	"strconv"
	"time"

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
	serviceContexts []*router.ServiceContext
	childServer     map[int]*WebServer
	htmls           *HTMLServer
	viewport        int
	serverip        string
	port            int
	socketport      int
}

func NewWebServer() *WebServer {
	config.INITSERVER = true
	ws := &WebServer{
		childServer:     make(map[int]*WebServer),
		serviceContexts: make([]*router.ServiceContext, 0),
	}
	ws.AddIService(&server.SystemManage{})
	return ws
}
func (own *WebServer) AddServiceContext(sc *router.ServiceContext) {
	sc.Router.AddServerRouters(release.Routers()...)
	own.serviceContexts = append(own.serviceContexts, sc)
	sc.RunNotify = func(nsc *router.ServiceContext) {
		if nsc.IsRun() {
			for _, ctx := range router.GetContexts() {
				if !ctx.IsRun() {
					return
				}
			}
			fmt.Println("全部服务启动成功，开始合并服务。。。")
			for _, ctx := range router.GetContexts() {
				for _, cfg := range ctx.Config.AttachServices {
					if cfg.Address != "" && cfg.Port != 0 {
						time.Sleep(200 * time.Millisecond)
						ctx.SetAttachServiceAddress(cfg.Name)
						time.Sleep(200 * time.Millisecond)
						err := ctx.RegisterObserve(&public.Observe{})
						if err != nil {
							msg := ctx.Service.Name + "服务中合并服务" + cfg.Name + ",地址:" + cfg.Address + ":" + strconv.Itoa(cfg.Port) + "异常，异常信息：" + err.Error()
							fmt.Println(msg)
						} else {
							msg := ctx.Service.Name + "服务中合并服务" + cfg.Name + ",地址:" + cfg.Address + ":" + strconv.Itoa(cfg.Port) + "成功"
							fmt.Println(msg)
						}
					} else {
						msg := cfg.Name + "服务待合并,但未设置地址和端口，请设置地址的端口号"
						fmt.Println(msg)
					}
				}
			}
			//fmt.Println("合并服务完成")
		}
	}
}
func (own *WebServer) AddIService(service types.IService) {
	sc := router.NewServiceContext(service)
	own.AddServiceContext(sc)
}

func (own *WebServer) Start() {
	config.INITSERVER = true
	own.initServer()
	group := service.NewServiceGroup()
	defer group.Stop()
	for _, ctx := range own.serviceContexts {
		for _, server := range ctx.GetServers() {
			if server != nil {
				group.Add(server)
			}
		}
	}
	group.Add(own.htmls)
	config.INITSERVER = false
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
		own.NewInternalServer(ctx)
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
func (own *WebServer) NewInternalServer(ctx *router.ServiceContext) {
	if own.socketport > 0 {
		ss := socket.NewServer(ctx)
		ctx.SetSocketServer(ss)
	}
}
