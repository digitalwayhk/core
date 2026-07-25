package run

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/server/api/public"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/trans"
	"github.com/digitalwayhk/core/pkg/server/trans/rest"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
)

//go:embed dist
var html embed.FS

type HTMLServer struct {
	Port                int
	services            []*router.ServiceRouter
	Isstart             chan bool
	Parent              *WebServer
	lifecycleMu         sync.Mutex
	server              *http.Server
	stopCh              chan struct{}
	stopOnce            sync.Once
	stopped             bool
	manageAuthAuthority *manageAuthAuthority
	handler             http.Handler
	prepared            bool
}

func (own *HTMLServer) SetManageAuthAuthority(authority *manageAuthAuthority) {
	own.manageAuthAuthority = authority
}

func NewHTMLServer(port int) *HTMLServer {
	ser := &HTMLServer{
		services: make([]*router.ServiceRouter, 0),
		Port:     port,
		Isstart:  make(chan bool, 1),
		stopCh:   make(chan struct{}),
	}
	return ser
}
func (own *HTMLServer) AddServiceRouter(sr *router.ServiceRouter) {
	own.services = append(own.services, sr)
}

var qs = &public.QueryService{}

var manageAuthProxyRoutes = []struct {
	external string
	internal string
}{
	{"/api/servermanage/testtoken", "/api/servermanage/testtoken"},
	{"/api/casdoor", "/api/casdoor"},
	{"/callback", "/api/casdoor/callback"},
	{"/api/refresh", "/api/refresh"},
}

func (own *HTMLServer) Prepare() error {
	own.lifecycleMu.Lock()
	defer own.lifecycleMu.Unlock()
	if own.prepared && own.handler != nil {
		return nil
	}
	sfsys, _ := fs.Sub(swagger, "swagger")
	mux := http.NewServeMux()
	mux.Handle("/swagger/", http.StripPrefix("/swagger/", http.FileServer(http.FS(sfsys))))
	mux.Handle(webBootstrapPath, newWebBootstrapHandler(own.manageAuthAuthority))
	if err := own.mountManageAuthProxy(mux); err != nil {
		return err
	}

	for _, service := range own.services {
		for _, router := range service.GetTypeRouters(types.ManageType) {
			mux.Handle(router.GetPath()+"/"+service.Service.Service.Name, htmlHandler(service))
		}
		for _, router := range service.GetTypeRouters(types.ServerManagerType) {
			if router.GetStructName() != "QueryService" {
				mux.Handle(router.GetPath()+"/"+service.Service.Service.Name, htmlHandler(service))
			}
		}
	}
	mux.Handle("/api/openapi", htmlHandler(own.services...))
	mux.HandleFunc(qs.RouterInfo().GetPath(), func(w http.ResponseWriter, r *http.Request) {
		data, _ := qs.Do(nil)
		httpx.OkJson(w, data)
	})
	var isview = true
	if own.Parent != nil {
		ops := own.Parent.GetServerOptions()
		for n, op := range ops {
			if op != nil && op.Demo != nil {
				if op.Demo.Pattern != "" {
					mux.Handle("/"+op.Demo.Pattern+"/", http.StripPrefix("/"+op.Demo.Pattern+"/", http.FileServer(http.FS(op.Demo.File))))
				} else {
					mux.Handle("/", http.FileServer(http.FS(op.Demo.File)))
					isview = false
				}
				logx.Infow("demo_server_ready",
					logx.Field("service", n),
					logx.Field("port", own.Port),
					logx.Field("pattern", op.Demo.Pattern),
				)
			}
		}
	}
	if isview {
		fsys, _ := fs.Sub(html, "dist")
		mux.Handle("/", http.FileServer(http.FS(fsys)))
		// 🔧 设置404默认路由 - 必须在最后添加
		// http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// 	http.ServeFile(w, r, "dist/index.html")
		// })
		logx.Infow("development_view_ready", logx.Field("port", own.Port))
	}
	own.handler = mux
	own.prepared = true
	return nil
}

func (own *HTMLServer) mountManageAuthProxy(mux *http.ServeMux) error {
	authority := own.manageAuthAuthority
	if authority == nil {
		return nil
	}
	if authority.router == nil || authority.context == nil {
		return fmt.Errorf("Manage Auth 权威服务 %s 未就绪", normalizeBootstrapAuthorityService(authority))
	}
	for _, route := range manageAuthProxyRoutes {
		info := authority.router.GetRouter(route.internal)
		if info == nil {
			return fmt.Errorf("Manage Auth 权威服务 %s 缺少同源认证路由 %s",
				normalizeBootstrapAuthorityService(authority), route.internal)
		}
		handler, err := rest.NewExternalRouterHandler(authority.router, info)
		if err != nil {
			return fmt.Errorf("构造同源认证路由 %s 失败: %w", route.external, err)
		}
		mux.Handle(route.external, rewriteExternalRoutePath(handler, route.internal))
	}
	return nil
}

func rewriteExternalRoutePath(next http.Handler, internalPath string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		cloned := request.Clone(request.Context())
		urlCopy := *request.URL
		urlCopy.Path = internalPath
		urlCopy.RawPath = ""
		cloned.URL = &urlCopy
		cloned.RequestURI = internalPath
		if urlCopy.RawQuery != "" {
			cloned.RequestURI += "?" + urlCopy.RawQuery
		}
		next.ServeHTTP(w, cloned)
	})
}

func (own *HTMLServer) Handler() http.Handler {
	own.lifecycleMu.Lock()
	defer own.lifecycleMu.Unlock()
	return own.handler
}

func (own *HTMLServer) Start() {
	if own.Port == 0 {
		return
	}
	var run bool
	select {
	case run = <-own.Isstart:
	case <-own.stopCh:
		return
	}
	if !run {
		return
	}
	handler := own.Handler()
	if handler == nil {
		return
	}
	server := &http.Server{
		Addr:    ":" + strconv.Itoa(own.Port),
		Handler: handler,
	}
	own.lifecycleMu.Lock()
	if own.stopped {
		own.lifecycleMu.Unlock()
		return
	}
	own.server = server
	own.lifecycleMu.Unlock()
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logx.Errorf("HTML 服务运行失败，端口：%d，错误：%v", own.Port, err)
	}
}
func (own *HTMLServer) Stop() {
	own.stopOnce.Do(func() {
		close(own.stopCh)
		own.lifecycleMu.Lock()
		own.stopped = true
		server := own.server
		own.lifecycleMu.Unlock()
		if server == nil {
			return
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logx.Errorf("HTML 服务关闭失败，端口：%d，错误：%v", own.Port, err)
		}
	})
}

func htmlHandler(service ...*router.ServiceRouter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		url := strings.Trim(r.RequestURI, " ")
		if url == "/api/openapi" {
			httpx.OkJson(w, GetOpenApi(r, service...))
			return
		}
		if r.Method == "POST" {
			last := strings.LastIndex(url, "/")
			servicename := url[last+1:]
			index := strings.Index(servicename, "?")
			if index > 0 {
				servicename = servicename[:index]
			}
			path := url[:last]
			ss := getService(servicename, service)
			req := router.NewRequest(ss, r)
			ip := utils.ClientPublicIP(r, ss.Service.Config.TrustedProxies...)
			err := trans.VerifyIPWhiteList(ss.Service.Config, ip)
			if err != nil {
				httpx.OkJson(w, req.NewResponse(nil, err))
				return
			}
			req.SetPath(path)
			if item := ss.GetRouter(path); item != nil {
				res := item.Exec(req)
				if item.ResponseHandlerFunc == nil {
					httpx.OkJson(w, res)
				} else {
					item.ResponseHandlerFunc(w, r, res)
				}
			} else {
				httpx.OkJson(w, req.NewResponse(nil, errors.New(req.GetPath()+"未找到对应的接口！")))
			}
		}
	}
}
func getService(name string, ss []*router.ServiceRouter) *router.ServiceRouter {
	for _, s := range ss {
		if s.Service.Config.Name == name {
			return s
		}
	}
	return nil
}
