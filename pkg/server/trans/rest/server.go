package rest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/trans"
	"github.com/digitalwayhk/core/pkg/server/trans/websocket/melody"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest"
)

type Server struct {
	*rest.Server
	context     *router.ServiceContext
	IsWebSocket bool
	IsCors      bool
	stateMu     sync.Mutex
	lifecycleMu sync.Mutex
	httpServer  *http.Server
	stopCh      chan struct{}
	stopOnce    sync.Once
	stopped     bool
}

// resolveRouteAuthPolicy 返回路由所属认证域。
func resolveRouteAuthPolicy(
	rou *router.ServiceRouter,
	path string,
) (config.AuthSecret, types.AuthType) {
	auth := rou.Service.Config.Auth
	authType := types.AuthTypeUser
	if rou.HasRouter(path, types.ServerManagerType) {
		auth = rou.Service.Config.ServerManageAuth
		authType = types.AuthTypeServerManage
	} else if rou.HasRouter(path, types.ManageType) {
		auth = rou.Service.Config.ManageAuth
		authType = types.AuthTypeManage
	}
	return auth, authType
}

func NewServer(context *router.ServiceContext, isWebSocket, isCors bool, origin ...string) (*Server, error) {
	options, err := restRunOptions(isCors, origin)
	if err != nil {
		return nil, err
	}
	ser := &Server{
		context: context,
		stopCh:  make(chan struct{}),
	}
	ser.IsWebSocket = isWebSocket
	if ser.IsWebSocket {
		context.Config.Timeout = 0
	}
	ser.IsCors = isCors
	ser.Server = rest.MustNewServer(context.Config.RestConf, options...)
	if err := ser.register(); err != nil {
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
		logx.Errorw("service_start_failed",
			logx.Field("service", own.context.Service.Name),
			logx.Field("port", own.context.Config.Port),
			logx.Field("error", "port already in use"),
		)
		return
	}
	go own.checkRun()
	logx.Infow("service_starting",
		logx.Field("service", own.context.Config.Name),
		logx.Field("host", own.context.Config.Host),
		logx.Field("port", own.context.Config.Port),
		logx.Field("websocket", own.IsWebSocket),
	)
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
	})
}
func (own *Server) register() error {
	routers := own.context.Router.GetRouters()
	count := len(routers)
	logx.Debugw("routes_registering",
		logx.Field("service", own.context.Config.Name),
		logx.Field("route_count", count),
	)
	for _, api := range routers {
		if err := handers(own, api); err != nil {
			return err
		}
	}
	if own.IsWebSocket {
		own.websocket()
		//own.websocketauth()
	}
	logx.Debugw("routes_registered",
		logx.Field("service", own.context.Config.Name),
		logx.Field("route_count", count),
	)
	return nil
}

func handers(own *Server, api *types.RouterInfo) error {
	path := api.GetPath()
	var handler http.Handler = http.HandlerFunc(RouteHandler(own.context.Router))
	if api.GetAuth() {
		auth, authType := resolveRouteAuthPolicy(own.context.Router, path)
		handler = authRequestHandler(own.context, api, authType, handler)
		handler = internalJWTAuthorize(auth.AccessSecret, authType, handler)
	}
	handler = securityHeaders(externalRateLimitHandler(own.context, api, handler))

	own.Server.AddRoutes([]rest.Route{
		{
			Method:  api.GetMethod(),
			Path:    path,
			Handler: handler.ServeHTTP,
		},
	})
	logx.Debugw("route_registered",
		logx.Field("service", own.context.Config.Name),
		logx.Field("route", path),
		logx.Field("method", api.GetMethod()),
		logx.Field("auth", api.GetAuth()),
	)
	return nil
}

func externalRateLimitHandler(sc *router.ServiceContext, api *types.RouterInfo, next http.Handler) http.Handler {
	if next == nil {
		next = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			writePublicErrorContract(w, types.NewPublicError(types.ErrorKindUnavailable, 0, "", nil).PublicErrorContract())
		})
	}
	policy := api.GetExternalRateLimit()
	if policy == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isDirectLoopbackRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		if sc == nil || sc.Config == nil || sc.PublicRateLimiter == nil {
			writePublicErrorContract(w, types.NewPublicError(types.ErrorKindUnavailable, 0, "", nil).PublicErrorContract())
			return
		}
		clientIP := utils.ClientPublicIP(r, sc.Config.TrustedProxies...)
		if sc.PublicRateLimiter.Allow(api.GetPath(), clientIP, *policy) {
			next.ServeHTTP(w, r)
			return
		}
		serviceName := sc.Config.Name
		if sc.Service != nil && sc.Service.Name != "" {
			serviceName = sc.Service.Name
		}
		logx.Infow("external_api_rate_limited",
			logx.Field("service", serviceName),
			logx.Field("route", api.GetPath()),
			logx.Field("client", maskClientIP(clientIP)),
		)
		writePublicErrorContract(w, types.NewPublicError(types.ErrorKindRateLimited, 0, "", nil).PublicErrorContract())
	})
}

func isDirectLoopbackRequest(r *http.Request) bool {
	if r == nil || strings.TrimSpace(r.Header.Get("X-Forwarded-For")) != "" || strings.TrimSpace(r.Header.Get("X-Real-IP")) != "" {
		return false
	}
	host := strings.TrimSpace(r.RemoteAddr)
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	return err == nil && addr.Unmap().IsLoopback()
}

func maskClientIP(clientIP string) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(clientIP))
	if err != nil {
		return "unknown"
	}
	addr = addr.Unmap()
	bits := 64
	if addr.Is4() {
		bits = 24
	}
	return netip.PrefixFrom(addr, bits).Masked().String()
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
		if info.GetAuth() {
			_, authType := resolveRouteAuthPolicy(rou, info.GetPath())
			identity, _, err := verifiedRequestIdentity(r, rou.Service, authType)
			if err != nil {
				contract := types.ResolvePublicError(err)
				logAuthRequestDenied(rou.Service, info, authType, identity, contract)
				writePublicErrorContract(w, contract)
				return
			}
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
