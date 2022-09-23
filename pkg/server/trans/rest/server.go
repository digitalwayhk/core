package rest

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"

	"errors"
	"fmt"
	"net/http"

	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/httpx"
)

type Server struct {
	*rest.Server
	context *router.ServiceContext
}

func NewServer(context *router.ServiceContext) *Server {
	//webSocket 必须timeout=0
	context.Config.Timeout = 0
	ser := &Server{
		context: context,
		Server:  rest.MustNewServer(context.Config.RestConf),
	}
	ser.register()
	return ser
}
func (own *Server) Start() {
	pid := utils.ScanPort("tcp", own.context.Config.Host, own.context.Config.Port)
	if pid {
		panic(fmt.Sprintf("%s 服务的端口%d被占用,不能启动服务", own.context.Service.Name, own.context.Config.Port))
	}
	go checkRun(own.context)
	fmt.Printf("Starting %s server at %s:%d success\n", own.context.Config.Name, own.context.Config.Host, own.context.Config.Port)
	own.Server.Start()
}
func checkRun(context *router.ServiceContext) {
	for {
		time.Sleep(time.Millisecond * 10)
		pid := utils.ScanPort("tcp", context.Config.Host, context.Config.Port)
		if pid {
			//context.SetPid(pid)
			go context.SetRunState(true)
			return
		}
	}
}
func (own *Server) Stop() {
	own.context.SetRunState(false)
	own.Server.Stop()
}
func (own *Server) register() {
	own.websocket()
	own.websocketauth()
	routers := own.context.Router.GetRouters()
	count := len(routers)
	fmt.Println("===========================================================")
	fmt.Printf("%s Register Service Routes Start. \n", own.context.Config.Name)
	fmt.Println("Routes Count : " + strconv.Itoa(count))
	for _, api := range routers {
		handers(own, api)
	}
	fmt.Printf("%s Register Service Routes End. \n", own.context.Config.Name)
	fmt.Println("===========================================================")
}

func handers(own *Server, api *types.RouterInfo) {
	opts := make([]rest.RouteOption, 0)
	path := api.Path
	if api.Auth {
		if own.context.Router.HasRouter(path, types.ManageType) {
			opts = append(opts, rest.WithJwt(own.context.Config.ManageAuth.AccessSecret))
		} else {
			opts = append(opts, rest.WithJwt(own.context.Config.Auth.AccessSecret))
		}
	}
	own.Server.AddRoutes([]rest.Route{
		{
			Method:  api.Method,
			Path:    path,
			Handler: routeHandler(own.context.Router),
		},
	}, opts...)
	fmt.Printf("register route: %s \n", path)
}
func (own *Server) RegisterHandlers(routers []*types.RouterInfo) {
	for _, rou := range routers {
		handers(own, rou)
	}
}

func routeHandler(rou *router.ServiceRouter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req := router.NewRequest(rou, r)
		info := rou.GetRouter(req.GetPath())
		if info != nil {
			res := info.Exec(req)
			httpx.OkJson(w, res)
		} else {
			httpx.OkJson(w, req.NewResponse(errors.New(req.GetPath()+"未找到对应的接口！"), nil))
		}
	}
}

func (own *Server) Send(payload *types.PayLoad) ([]byte, error) {
	if payload.TargetAddress == "" {
		return nil, errors.New("TargetAddress is nil")
	}
	values, err := json.Marshal(payload.Instance)
	if err != nil {
		return nil, err
	}
	path := payload.TargetAddress + ":" + fmt.Sprintf("%d", payload.TargetPort) + payload.TargetPath
	values, err = PostJson(path, values)
	if err != nil {
		return nil, err
	}
	return values, nil
}

func (own *Server) websocket() {
	hub := NewHub()
	hub.serviceContext = own.context
	go hub.Run()
	own.context.Hub = hub
	own.Server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/ws",
		Handler: websocketHandler(own.context),
	})
}
func (own *Server) websocketauth() {
	opts := make([]rest.RouteOption, 0)
	opts = append(opts, rest.WithJwt(own.context.Config.Auth.AccessSecret))
	own.Server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/wsauth",
		Handler: websocketHandler(own.context),
	}, opts...)
}
func websocketHandler(sc *router.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ServeWs(sc.Hub.(*Hub), w, r)
	}
}
func (own *Server) GetIPandPort() (string, int) {
	return own.context.Config.Host, own.context.Config.Port
}
