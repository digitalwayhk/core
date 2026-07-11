package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/safe/casdoor"
	"github.com/digitalwayhk/core/pkg/server/safe/logto"
	"github.com/digitalwayhk/core/pkg/server/trans"
	"github.com/digitalwayhk/core/pkg/server/trans/websocket/melody"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
)

type Server struct {
	*rest.Server
	context       *router.ServiceContext
	logtoHandlers *logto.HandlerFactory
	IsWebSocket   bool
	IsCors        bool
	stateMu       sync.Mutex
	lifecycleMu   sync.Mutex
	httpServer    *http.Server
	stopCh        chan struct{}
	stopOnce      sync.Once
	stopped       bool
}

func NewServer(context *router.ServiceContext, isWebSocket, isCors bool, origin ...string) (*Server, error) {
	options, err := restRunOptions(isCors, origin)
	if err != nil {
		return nil, err
	}
	ser := &Server{
		context:       context,
		logtoHandlers: logto.NewHandlerFactory(),
		stopCh:        make(chan struct{}),
	}
	ser.IsWebSocket = isWebSocket
	if ser.IsWebSocket {
		context.Config.Timeout = 0
	}
	ser.IsCors = isCors
	ser.Server = rest.MustNewServer(context.Config.RestConf, options...)
	if err := ser.register(); err != nil {
		ser.logtoHandlers.Close()
		return nil, err
	}
	return ser, nil
}

func restRunOptions(isCors bool, origins []string) ([]rest.RunOption, error) {
	if !isCors {
		return nil, nil
	}

	origins = normalizeCorsOrigins(origins)
	if len(origins) == 0 {
		return nil, errors.New("at least one CORS origin is required")
	}

	return []rest.RunOption{rest.WithCors(origins...)}, nil
}

func normalizeCorsOrigins(origins []string) []string {
	normalized := make([]string, 0, len(origins))
	for _, origin := range origins {
		if origin = strings.TrimSpace(origin); origin != "" {
			normalized = append(normalized, origin)
		}
	}
	return normalized
}
func (own *Server) Start() {
	own.lifecycleMu.Lock()
	if own.stopped {
		own.lifecycleMu.Unlock()
		return
	}
	own.lifecycleMu.Unlock()

	pid := utils.ScanPort("tcp", own.context.Config.Host, own.context.Config.Port)
	if pid {
		panic(fmt.Sprintf("%s 服务的端口%d被占用,不能启动服务", own.context.Service.Name, own.context.Config.Port))
	}
	go own.checkRun()
	s1 := fmt.Sprintf("Starting %s server at %s:%d success\n", own.context.Config.Name, own.context.Config.Host, own.context.Config.Port)
	if own.IsWebSocket {
		s2 := fmt.Sprintf("Starting %s websocket at %s:%d success,path:%s:%d/ws \n", own.context.Config.Name, own.context.Config.Host, own.context.Config.Port, own.context.Config.Host, own.context.Config.Port)
		//s3 := fmt.Sprintf("Starting %s websocket auth at %s:%d success,path:%s:%d/wsauth \n", own.context.Config.Name, own.context.Config.Host, own.context.Config.Port, own.context.Config.Host, own.context.Config.Port)
		fmt.Print(s1, s2)
	} else {
		fmt.Print(s1)
	}
	own.Server.StartWithOpts(func(server *http.Server) {
		own.lifecycleMu.Lock()
		own.httpServer = server
		stopped := own.stopped
		own.lifecycleMu.Unlock()
		if stopped {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdownCtx)
		}
	})
}
func (own *Server) checkRun() {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-own.stopCh:
			return
		case <-ticker.C:
			if utils.ScanPort("tcp", own.context.Config.Host, own.context.Config.Port) {
				own.stateMu.Lock()
				own.lifecycleMu.Lock()
				stopped := own.stopped
				own.lifecycleMu.Unlock()
				if !stopped {
					own.context.SetRunState(true)
				}
				own.stateMu.Unlock()
				return
			}
		}
	}
}
func (own *Server) Stop() {
	own.stopOnce.Do(func() {
		close(own.stopCh)
		own.stateMu.Lock()
		own.lifecycleMu.Lock()
		own.stopped = true
		server := own.httpServer
		own.lifecycleMu.Unlock()

		own.context.SetRunState(false)
		own.stateMu.Unlock()
		if server != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logx.Errorf("REST 服务关闭失败，服务：%s，错误：%v", own.context.Service.Name, err)
			}
			cancel()
		}
		own.logtoHandlers.Close()
	})
}
func (own *Server) register() error {
	routers := own.context.Router.GetRouters()
	count := len(routers)
	fmt.Println("===========================================================")
	fmt.Printf("%s Register Service Routes Start. \n", own.context.Config.Name)
	fmt.Println("Routes Count : " + strconv.Itoa(count))
	for _, api := range routers {
		if err := handers(own, api); err != nil {
			return err
		}
	}
	if own.IsWebSocket {
		own.websocket()
		//own.websocketauth()
	}
	fmt.Printf("%s Register Service Routes End. \n", own.context.Config.Name)
	fmt.Println("===========================================================")
	return nil
}

func handers(own *Server, api *types.RouterInfo) error {
	opts := make([]rest.RouteOption, 0)
	path := api.Path
	handler := RouteHandler(own.context.Router)
	if api.Auth {
		if own.context.Router.HasRouter(path, types.ManageType) {
			if own.context.Config.ManageAuth.Logto.Enable {
				authHandler, err := own.newLogtoHandler(RouteHandler(own.context.Router), own.context.Config.ManageAuth.Logto)
				if err != nil {
					return fmt.Errorf("initialize manage Logto authentication: %w", err)
				}
				handler = authHandler.ServeHTTP
			} else if own.context.Config.ManageAuth.CasDoor.Enable {
				handler = casdoor.AuthHandler(RouteHandler(own.context.Router)).ServeHTTP
			} else {
				opts = append(opts, rest.WithJwt(own.context.Config.ManageAuth.AccessSecret))
			}

		} else {
			if own.context.Config.Auth.Logto.Enable {
				authHandler, err := own.newLogtoHandler(RouteHandler(own.context.Router), own.context.Config.Auth.Logto)
				if err != nil {
					return fmt.Errorf("initialize Logto authentication: %w", err)
				}
				handler = authHandler.ServeHTTP
			} else if own.context.Config.Auth.CasDoor.Enable {
				handler = casdoor.AuthHandler(RouteHandler(own.context.Router)).ServeHTTP
			} else {
				opts = append(opts, rest.WithJwt(own.context.Config.Auth.AccessSecret))
			}
		}
	}
	handler = securityHeaders(http.HandlerFunc(handler)).ServeHTTP

	own.Server.AddRoutes([]rest.Route{
		{
			Method:  api.Method,
			Path:    path,
			Handler: handler,
		},
	}, opts...)
	fmt.Printf("register auth: %t ,method: %s ,route: %s \n", api.Auth, api.Method, path)
	return nil
}

func newLogtoHandler(next http.HandlerFunc, cfg config.LogtoConfig) (http.Handler, error) {
	return logto.NewAuthHandler(next, logto.AuthConfig{
		Issuer:           cfg.Issuer,
		ExpectedAudience: cfg.ExpectedAudience,
	})
}

func (own *Server) newLogtoHandler(next http.HandlerFunc, cfg config.LogtoConfig) (http.Handler, error) {
	return own.logtoHandlers.NewAuthHandler(next, logto.AuthConfig{
		Issuer:           cfg.Issuer,
		ExpectedAudience: cfg.ExpectedAudience,
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers := w.Header()
		setHeaderIfEmpty(headers, "X-Content-Type-Options", "nosniff")
		setHeaderIfEmpty(headers, "Referrer-Policy", "no-referrer")
		setHeaderIfEmpty(headers, "X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func setHeaderIfEmpty(headers http.Header, name, value string) {
	if headers.Get(name) == "" {
		headers.Set(name, value)
	}
}

func (own *Server) RegisterHandlers(routers []*types.RouterInfo) {
	for _, rou := range routers {
		if err := handers(own, rou); err != nil {
			panic(err)
		}
	}
}

func RouteHandler(rou *router.ServiceRouter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		req := router.NewRequest(rou, r)
		if req == nil {
			writeErrorResponse(w, StatusUnauthorized, "authentication failed", nil)
			return
		}
		ip := utils.ClientPublicIP(r, rou.Service.Config.TrustedProxies...)

		// IP 白名单验证
		err := trans.VerifyIPWhiteList(rou.Service.Config, ip)
		if err != nil {
			writeErrorResponse(w, StatusForbidden, "IP not in whitelist", err)
			return
		}

		info := rou.GetRouter(req.GetPath())
		if info == nil {
			writeErrorResponse(w, StatusNotFound, "Route not found: "+req.GetPath(), nil)
			return
		}

		// 执行路由处理
		res := info.Exec(req)

		// 自定义响应处理
		if info.ResponseHandlerFunc != nil {
			info.ResponseHandlerFunc(w, r, res)
			return
		}

		// 标准响应处理
		HandleResponse(w, res)
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
		Handler: securityHeaders(websocketHandler(own.context)).ServeHTTP,
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

		ip := utils.ClientPublicIP(r, sc.Config.TrustedProxies...)
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

	}
}
