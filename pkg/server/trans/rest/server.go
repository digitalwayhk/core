package rest

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/safe/casdoor"
	"github.com/digitalwayhk/core/pkg/server/safe/logto"
	"github.com/digitalwayhk/core/pkg/server/trans"
	"github.com/digitalwayhk/core/pkg/server/trans/websocket/melody"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"

	"errors"
	"fmt"
	"net/http"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/rest/httpx"
)

type Server struct {
	*rest.Server
	context     *router.ServiceContext
	IsWebSocket bool
	IsCors      bool
}

func NewServer(context *router.ServiceContext, isWebSocket, isCors bool, origin ...string) *Server {
	ser := &Server{
		context: context,
	}
	ser.IsWebSocket = isWebSocket
	if ser.IsWebSocket {
		context.Config.Timeout = 0
	}
	ser.IsCors = isCors
	if ser.IsCors {
		ser.Server = rest.MustNewServer(context.Config.RestConf, rest.WithCors())
	} else {
		ser.Server = rest.MustNewServer(context.Config.RestConf)
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
	s1 := fmt.Sprintf("Starting %s server at %s:%d success\n", own.context.Config.Name, own.context.Config.Host, own.context.Config.Port)
	if own.IsWebSocket {
		s2 := fmt.Sprintf("Starting %s websocket at %s:%d success,path:%s:%d/ws \n", own.context.Config.Name, own.context.Config.Host, own.context.Config.Port, own.context.Config.Host, own.context.Config.Port)
		//s3 := fmt.Sprintf("Starting %s websocket auth at %s:%d success,path:%s:%d/wsauth \n", own.context.Config.Name, own.context.Config.Host, own.context.Config.Port, own.context.Config.Host, own.context.Config.Port)
		fmt.Print(s1, s2)
	} else {
		fmt.Print(s1)
	}
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
	routers := own.context.Router.GetRouters()
	count := len(routers)
	fmt.Println("===========================================================")
	fmt.Printf("%s Register Service Routes Start. \n", own.context.Config.Name)
	fmt.Println("Routes Count : " + strconv.Itoa(count))
	for _, api := range routers {
		handers(own, api)
	}
	if own.IsWebSocket {
		own.websocket()
		//own.websocketauth()
	}
	fmt.Printf("%s Register Service Routes End. \n", own.context.Config.Name)
	fmt.Println("===========================================================")
}

func handers(own *Server, api *types.RouterInfo) {
	opts := make([]rest.RouteOption, 0)
	path := api.Path
	handler := routeHandler(own.context.Router)
	if api.Auth {
		if own.context.Router.HasRouter(path, types.ManageType) {
			if own.context.Config.ManageAuth.Logto.Enable {
				handler = logto.AuthHandler(routeHandler(own.context.Router), own.context.Config.ManageAuth.Logto.Issuer, own.context.Config.ManageAuth.Logto.ExpectedAudience).ServeHTTP
			} else if own.context.Config.ManageAuth.CasDoor.Enable {
				handler = casdoor.AuthHandler(routeHandler(own.context.Router)).ServeHTTP
			} else {
				opts = append(opts, rest.WithJwt(own.context.Config.ManageAuth.AccessSecret))
			}

		} else {
			if own.context.Config.Auth.Logto.Enable {
				handler = logto.AuthHandler(routeHandler(own.context.Router), own.context.Config.Auth.Logto.Issuer, own.context.Config.Auth.Logto.ExpectedAudience).ServeHTTP
			} else if own.context.Config.Auth.CasDoor.Enable {
				handler = casdoor.AuthHandler(routeHandler(own.context.Router)).ServeHTTP
			} else {
				opts = append(opts, rest.WithJwt(own.context.Config.Auth.AccessSecret))
			}
		}
	}

	own.Server.AddRoutes([]rest.Route{
		{
			Method:  api.Method,
			Path:    path,
			Handler: handler,
		},
	}, opts...)
	fmt.Printf("register auth: %t ,method: %s ,route: %s \n", api.Auth, api.Method, path)
}
func (own *Server) RegisterHandlers(routers []*types.RouterInfo) {
	for _, rou := range routers {
		handers(own, rou)
	}
}

func routeHandler(rou *router.ServiceRouter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req := router.NewRequest(rou, r)
		ip := utils.ClientPublicIP(r)
		err := trans.VerifyIPWhiteList(rou.Service.Config, ip)
		if err != nil {
			httpx.OkJson(w, req.NewResponse(nil, err))
			return
		}
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
	//logx.Info("http Send :" + utils.PrintObj(payload))
	values, err := json.Marshal(payload.Instance)
	if err != nil {
		return nil, err
	}
	path := payload.TargetAddress + ":" + fmt.Sprintf("%d", payload.TargetPort) + payload.TargetPath
	logx.Info(path)
	if payload.HttpMethod == http.MethodGet {
		args := ""
		utils.ForEach(payload.Instance, func(key string, value interface{}) {
			v := utils.ConvertToString(value)
			if v != "" {
				args += "&" + key + "=" + v
			}
		})
		if args != "" {
			path = path + "?" + args[1:]
		}
		values, err = HttpGet(path, payload)
		if err != nil {
			return nil, err
		}
	}
	if payload.HttpMethod == http.MethodPost || payload.HttpMethod == "" {
		values, err = PostJson(path, values, payload)
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}

// func (own *Server) websocket() {
// 	hub := NewHub()
// 	hub.serviceContext = own.context
// 	go hub.Run()
// 	own.context.Hub = hub
// 	own.Server.AddRoute(rest.Route{
// 		Method:  http.MethodGet,
// 		Path:    "/ws",
// 		Handler: websocketHandler(own.context),
// 	})
// 	//fmt.Printf("register websocket: %s \n", own.context.Config.RunIp+"/ws")
// }

// func (own *Server) websocketauth() {
// 	opts := make([]rest.RouteOption, 0)
// 	opts = append(opts, rest.WithJwt(own.context.Config.Auth.AccessSecret))
// 	//opts = append(opts, rest.WithTimeout(0))
// 	own.Server.AddRoute(rest.Route{
// 		Method:  http.MethodGet,
// 		Path:    "/wsauth",
// 		Handler: websocketHandler(own.context),
// 	}, opts...)
// 	//fmt.Printf("register websocket: %s \n", own.context.Config.RunIp+"/wsauth")
// }

//	func websocketHandler(sc *router.ServiceContext) http.HandlerFunc {
//		return func(w http.ResponseWriter, r *http.Request) {
//			ip := utils.ClientPublicIP(r)
//			err := trans.VerifyIPWhiteList(sc.Config, ip)
//			if err != nil {
//				httpx.OkJson(w, err)
//				return
//			}
//			ServeWs(sc.Hub.(*Hub), w, r)
//		}
//	}
func (own *Server) GetIPandPort() (string, int) {
	return own.context.Config.Host, own.context.Config.Port
}

func (own *Server) websocket() {
	melodyManager := melody.NewMelodyManager(own.context)
	own.context.Hub = melodyManager

	// 🔧 修复：为WebSocket路由单独设置超时
	opts := make([]rest.RouteOption, 0)
	opts = append(opts, rest.WithTimeout(0)) // 只对WebSocket路由禁用超时

	own.Server.AddRoute(rest.Route{
		Method:  http.MethodGet,
		Path:    "/ws",
		Handler: websocketHandler(own.context),
	}, opts...)
}

// func (own *Server) websocketauth() {
// 	opts := make([]rest.RouteOption, 0)
// 	opts = append(opts, rest.WithJwt(own.context.Config.Auth.AccessSecret))
// 	opts = append(opts, rest.WithTimeout(0)) // 添加：为认证WebSocket路由也禁用超时

// 	own.Server.AddRoute(rest.Route{
// 		Method:  http.MethodGet,
// 		Path:    "/wsauth",
// 		Handler: websocketHandler(own.context),
// 	}, opts...)
// }

func websocketHandler(sc *router.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		//startTime := time.Now()

		ip := utils.ClientPublicIP(r)
		melodyManager := sc.Hub.(*melody.MelodyManager)
		if melodyManager == nil {
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			return
		}

		// 检查当前连接数
		currentConn := melodyManager.GetConnectionCounter().Get()
		if currentConn >= melodyManager.GetMaxConnections() {
			logx.Errorf("连接数已达上限，拒绝新连接: %s, 当前连接: %d", ip, currentConn)
			http.Error(w, "Service Busy", http.StatusServiceUnavailable)
			return
		}

		// 连接频率限制
		limit := melodyManager.GetConnectionLimiter()
		if !limit.Allow(ip) {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}

		// IP验证
		if err := trans.VerifyIPWhiteList(sc.Config, ip); err != nil {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		melodyManager.ServeWS(w, r)
		// 🔧 添加：握手超时检测
		// done := make(chan struct{})
		// go func() {
		// 	defer close(done)
		// 	melodyManager.ServeWS(w, r)
		// }()

		// // 🔧 可选：监控握手时间
		// go func() {
		// 	select {
		// 	case <-done:
		// 		duration := time.Since(startTime)
		// 		if duration > 5*time.Second {
		// 			logx.Errorf("WebSocket握手耗时过长: %v, IP: %s", duration, ip)
		// 		}
		// 	case <-time.After(30 * time.Second): // 30秒握手超时
		// 		logx.Errorf("WebSocket握手超时: IP: %s", ip)
		// 		// 这里不能强制关闭，因为可能已经升级成功
		// 	}
		// }()
	}
}
