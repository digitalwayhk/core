package run

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/server/api/public"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/trans/rest"
	"github.com/digitalwayhk/core/pkg/server/types"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/rest/httpx"
)

//go:embed dist
var html embed.FS

// manageAuthProxyPaths 是 HTMLServer 同源转发的固定认证路径（不追加服务名）。
var manageAuthProxyPaths = []string{
	"/api/servermanage/testtoken",
	"/api/casdoor",
	"/api/casdoor/callback",
	"/api/refresh",
}

// manageAuthProxyPathSet 用于挂载 Manage/ServerManager 时跳过已由权威代理占用的路径。
var manageAuthProxyPathSet = func() map[string]struct{} {
	set := make(map[string]struct{}, len(manageAuthProxyPaths))
	for _, p := range manageAuthProxyPaths {
		set[p] = struct{}{}
	}
	return set
}()

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

// SetManageAuthAuthority 保存 Manage Auth 权威服务选择结果。
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

// htmlMuxMount 统一「先 reserve/check 后 Handle」，避免 ServeMux 对重复精确 pattern panic。
type htmlMuxMount struct {
	mux     *http.ServeMux
	pattern map[string]string // pattern -> owner
	handled map[string]bool
}

func newHTMLMuxMount(mux *http.ServeMux) *htmlMuxMount {
	return &htmlMuxMount{
		mux:     mux,
		pattern: make(map[string]string),
		handled: make(map[string]bool),
	}
}

// reserve 预占精确 pattern，不调用 Handle。同 owner 可重复 reserve；异 owner 冲突。
func (m *htmlMuxMount) reserve(pattern, owner string) error {
	if m == nil || strings.TrimSpace(pattern) == "" {
		return fmt.Errorf("HTMLServer 无效路由 pattern")
	}
	if prev, ok := m.pattern[pattern]; ok && prev != owner {
		return fmt.Errorf("HTMLServer 路径冲突: path=%s owner=%s refused=%s", pattern, prev, owner)
	}
	m.pattern[pattern] = owner
	return nil
}

// handle 在 pattern 空闲或已由同 owner reserve 时注册 Handler；异 owner 或二次 Handle fail-closed。
func (m *htmlMuxMount) handle(pattern, owner string, h http.Handler) error {
	if m == nil || m.mux == nil {
		return fmt.Errorf("HTMLServer mux 未就绪")
	}
	if strings.TrimSpace(pattern) == "" {
		return fmt.Errorf("HTMLServer 无效路由 pattern")
	}
	if prev, ok := m.pattern[pattern]; ok && prev != owner {
		return fmt.Errorf("HTMLServer 路径冲突: path=%s owner=%s refused=%s", pattern, prev, owner)
	}
	if m.handled[pattern] {
		return fmt.Errorf("HTMLServer 路径重复注册: path=%s owner=%s", pattern, owner)
	}
	m.pattern[pattern] = owner
	m.mux.Handle(pattern, h)
	m.handled[pattern] = true
	return nil
}

// Prepare 预构建并缓存完整 HTTP Handler（含同源认证代理、Public/Private、Manage 与 SPA）。
// 存在 Manage Auth 权威时必须解析四个固定路径；缺少 Router 时 fail closed。
// 所有精确 pattern 经 htmlMuxMount 登记，冲突 fail-closed，禁止 ServeMux panic。
func (own *HTMLServer) Prepare() error {
	own.lifecycleMu.Lock()
	defer own.lifecycleMu.Unlock()
	if own.prepared && own.handler != nil {
		return nil
	}

	mux := http.NewServeMux()
	mount := newHTMLMuxMount(mux)

	queryPath := qs.RouterInfo().GetPath()
	// 固定系统路径先 reserve，业务路由不得抢占。
	for _, item := range []struct {
		pattern string
		owner   string
	}{
		{"/api/openapi", "system:openapi"},
		{webBootstrapPath, "system:bootstrap"},
		{queryPath, "system:queryservice"},
		{"/swagger/", "system:swagger"},
	} {
		if err := mount.reserve(item.pattern, item.owner); err != nil {
			return err
		}
	}

	sfsys, _ := fs.Sub(swagger, "swagger")
	if err := mount.handle("/swagger/", "system:swagger", http.StripPrefix("/swagger/", http.FileServer(http.FS(sfsys)))); err != nil {
		return err
	}

	if err := own.mountManageAuthProxy(mount); err != nil {
		return err
	}
	// 无缓存运行时 Bootstrap：每次请求现场选择模式，不签发 Token。
	if err := mount.handle(webBootstrapPath, "system:bootstrap", newWebBootstrapHandler(own.manageAuthAuthority)); err != nil {
		return err
	}

	if err := own.mountServiceAPIRoutes(mount); err != nil {
		return err
	}

	if err := mount.handle("/api/openapi", "system:openapi", htmlOpenAPIHandler(own.services...)); err != nil {
		return err
	}
	if err := mount.handle(queryPath, "system:queryservice", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := qs.Do(nil)
		httpx.OkJson(w, data)
	})); err != nil {
		return err
	}
	var isview = true
	if own.Parent != nil {
		ops := own.Parent.GetServerOptions()
		for n, op := range ops {
			if op != nil && op.Demo != nil {
				if op.Demo.Pattern != "" {
					demoPattern := "/" + op.Demo.Pattern + "/"
					if err := mount.handle(demoPattern, "demo:"+n, http.StripPrefix("/"+op.Demo.Pattern+"/", http.FileServer(http.FS(op.Demo.File)))); err != nil {
						return err
					}
				} else {
					if err := mount.handle("/", "demo:"+n, http.FileServer(http.FS(op.Demo.File))); err != nil {
						return err
					}
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
		fsys, err := fs.Sub(html, "dist")
		if err != nil {
			return fmt.Errorf("load embed dist: %w", err)
		}
		if err := mount.handle("/", "system:spa", spaFallbackHandler(fsys)); err != nil {
			return err
		}
		logx.Infow("development_view_ready", logx.Field("port", own.Port))
	}

	own.handler = mux
	own.prepared = true
	return nil
}

func (own *HTMLServer) mountManageAuthProxy(mount *htmlMuxMount) error {
	for _, path := range manageAuthProxyPaths {
		targets := make(map[string]http.Handler)
		addTarget := func(sc *router.ServiceContext, service *router.ServiceRouter) {
			if sc == nil || service == nil {
				return
			}
			name := serviceContextName(sc)
			if name == "" {
				return
			}
			info := service.GetRouter(path)
			if info == nil {
				return
			}
			targets[name] = rest.NewExternalRouterHandler(sc, info)
		}
		for _, service := range own.services {
			if service == nil || service.Service == nil {
				continue
			}
			addTarget(service.Service, service)
		}
		if own.manageAuthAuthority != nil {
			addTarget(own.manageAuthAuthority.context, own.manageAuthAuthority.router)
			authorityName := serviceContextName(own.manageAuthAuthority.context)
			if _, ok := targets[authorityName]; !ok {
				return fmt.Errorf("权威服务 %s 缺少同源认证路由 %s", authorityName, path)
			}
		}
		if len(targets) == 0 {
			continue
		}
		proxyPath := path
		if err := mount.handle(path, "auth-proxy", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			targetName := own.authProxyTargetName(proxyPath, r)
			handler := targets[targetName]
			if handler == nil {
				http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
				return
			}
			handler.ServeHTTP(w, r)
		})); err != nil {
			return err
		}
	}
	return nil
}

// authProxyTargetName 根据认证域选择实际签发或刷新 Token 的服务。
// 普通用户和 Manage 默认使用业务权威，也可由 service 明确目标；
// ServerManage TestToken 始终使用框架内置 server。
func (own *HTMLServer) authProxyTargetName(path string, r *http.Request) string {
	if r == nil {
		return ""
	}
	query := r.URL.Query()
	if path == "/api/servermanage/testtoken" && strings.TrimSpace(query.Get("type")) == "2" {
		return "server"
	}
	if requested := normalizeServiceName(query.Get("service")); requested != "" {
		if requested == "server" {
			return ""
		}
		return requested
	}
	if own.manageAuthAuthority == nil {
		return ""
	}
	return serviceContextName(own.manageAuthAuthority.context)
}

func serviceContextName(sc *router.ServiceContext) string {
	if sc == nil {
		return ""
	}
	if sc.Service != nil {
		if name := normalizeServiceName(sc.Service.Name); name != "" {
			return name
		}
	}
	if sc.Config != nil {
		return normalizeServiceName(sc.Config.Name)
	}
	return ""
}

// serverManageGetMenuPath 是 HTMLServer 唯一允许以规范无后缀挂载的通用 ServerManager 菜单路径。
const serverManageGetMenuPath = "/api/servermanage/getmenu"

// mountServiceAPIRoutes 挂载 Public/Private（Swagger 同源）、Manage 规范路径与受限 ServerManager。
// 安全链一律 NewExternalRouterHandler；禁止直接 Exec。
//
// Public/Private：同源挂载完整安全链，路径冲突 fail-closed。
//
// 规范无后缀仅允许：
//  1. 四条权威认证代理（由 mountManageAuthProxy 挂载，此处跳过）
//  2. GET /api/servermanage/getmenu（确定性选择 system server 或 ManageAuth 权威）
//  3. Manage 业务原始路径（通常含服务段，冲突时 first-wins + 兼容后缀）
//  4. 各服务 Public/Private 业务路径（冲突 fail-closed）
//
// 其它 ServerManager（queryconfig/transportstats 等）即使路径相同也只挂
// /{path}/{service} 兼容入口，禁止任意 first-wins 暴露到规范路径。
func (own *HTMLServer) mountServiceAPIRoutes(mount *htmlMuxMount) error {
	// path -> 已占用该规范 Manage 路径的服务名（跨服务 first-wins + 兼容后缀）
	manageCanonicalOwners := make(map[string]string)

	// 先确定性挂载 getmenu 规范路径（若存在）
	if err := own.mountCanonicalGetMenu(mount); err != nil {
		return err
	}

	for _, service := range own.services {
		if service == nil || service.Service == nil || service.Service.Config == nil {
			continue
		}
		sc := service.Service
		serviceName := strings.TrimSpace(sc.Config.Name)
		if serviceName == "" {
			continue
		}

		// --- Manage：规范路径 + 兼容后缀 ---
		for _, info := range service.GetTypeRouters(types.ManageType) {
			if info == nil {
				continue
			}
			path := info.GetPath()
			if path == "" || info.GetStructName() == "QueryService" {
				continue
			}
			if _, reserved := manageAuthProxyPathSet[path]; reserved {
				continue
			}
			if owner, ok := manageCanonicalOwners[path]; ok {
				if owner != serviceName {
					logx.Infow("htmlserver_manage_route_conflict_compat_only",
						logx.Field("path", path),
						logx.Field("owner", owner),
						logx.Field("compat", serviceName),
					)
					compatPath := path + "/" + serviceName
					if err := mount.handle(compatPath, "manage-compat:"+serviceName,
						stripServiceSuffixHandler(serviceName, rest.NewExternalRouterHandler(sc, info))); err != nil {
						return err
					}
				}
				continue
			}
			// 与系统固定路径等冲突时 fail-closed（不改变跨 Manage 服务 first-wins 规则）
			if err := mount.handle(path, "manage:"+serviceName, rest.NewExternalRouterHandler(sc, info)); err != nil {
				return err
			}
			manageCanonicalOwners[path] = serviceName
			compatPath := path + "/" + serviceName
			if err := mount.handle(compatPath, "manage-compat:"+serviceName,
				stripServiceSuffixHandler(serviceName, rest.NewExternalRouterHandler(sc, info))); err != nil {
				return err
			}
		}

		// --- ServerManager：默认仅兼容后缀；getmenu 规范已单独挂载 ---
		for _, info := range service.GetTypeRouters(types.ServerManagerType) {
			if info == nil {
				continue
			}
			path := info.GetPath()
			if path == "" || info.GetStructName() == "QueryService" {
				continue
			}
			if _, reserved := manageAuthProxyPathSet[path]; reserved {
				// 权威认证代理已占用，禁止再以兼容路径重复注册
				continue
			}
			// getmenu：规范路径已由 mountCanonicalGetMenu 挂载；各服务仍可保留兼容后缀
			if path == serverManageGetMenuPath {
				compatPath := path + "/" + serviceName
				if err := mount.handle(compatPath, "servermanage-compat:"+serviceName,
					stripServiceSuffixHandler(serviceName, rest.NewExternalRouterHandler(sc, info))); err != nil {
					return err
				}
				continue
			}
			// 其它 ServerManager：禁止挂规范无后缀，仅兼容 /{service}
			compatPath := path + "/" + serviceName
			if err := mount.handle(compatPath, "servermanage-compat:"+serviceName,
				stripServiceSuffixHandler(serviceName, rest.NewExternalRouterHandler(sc, info))); err != nil {
				return err
			}
		}

		// --- Public / Private：Swagger 同源；冲突 fail-closed ---
		if err := own.mountPublicPrivateRoutes(mount, service, sc, serviceName); err != nil {
			return err
		}
	}
	return nil
}

// mountPublicPrivateRoutes 将 Public+Private 以 NewExternalRouterHandler 挂到 HTML mux。
// 不挂 Manage；不直接 Exec。路径已被占用时返回错误（fail-closed）。
//
// system 服务 name=="server" 与 OpenAPI 文档集合一致：整服务跳过 Public/Private 同源挂载。
// 其 release/SystemManage 路由（如 GetMenu）只走 ServerManager 既有限制，不得再以 public 暴露。
func (own *HTMLServer) mountPublicPrivateRoutes(
	mount *htmlMuxMount,
	service *router.ServiceRouter,
	sc *router.ServiceContext,
	serviceName string,
) error {
	if isSystemServerService(sc, serviceName) {
		return nil
	}
	for _, pathType := range []types.ApiType{types.PublicType, types.PrivateType} {
		for _, info := range service.GetTypeRouters(pathType) {
			if info == nil {
				continue
			}
			path := info.GetPath()
			if path == "" || info.GetStructName() == "QueryService" {
				continue
			}
			kind := "public"
			if pathType == types.PrivateType {
				kind = "private"
			}
			if err := mount.handle(path, kind+":"+serviceName, rest.NewExternalRouterHandler(sc, info)); err != nil {
				return err
			}
		}
	}
	return nil
}

// isSystemServerService reports whether this is the framework system service "server",
// which OpenAPI and HTML same-origin Public/Private mounting both skip.
func isSystemServerService(sc *router.ServiceContext, serviceName string) bool {
	if strings.EqualFold(strings.TrimSpace(serviceName), "server") {
		return true
	}
	if sc == nil {
		return false
	}
	if sc.Service != nil && strings.EqualFold(strings.TrimSpace(sc.Service.Name), "server") {
		return true
	}
	if sc.Config != nil && strings.EqualFold(strings.TrimSpace(sc.Config.Name), "server") {
		return true
	}
	return false
}

// getMenuCandidate 是 getmenu 规范路径的候选服务。
type getMenuCandidate struct {
	name string
	sc   *router.ServiceContext
	info *types.RouterInfo
}

// mountCanonicalGetMenu 以确定性规则选择 getmenu 的规范路径 handler。
// 优先级：服务名 "server" > ManageAuthAuthority 服务名 > 按服务名字典序最小者。
// 若无一服务注册 getmenu，则跳过（不报错）。
func (own *HTMLServer) mountCanonicalGetMenu(mount *htmlMuxMount) error {
	var cands []getMenuCandidate
	for _, service := range own.services {
		if service == nil || service.Service == nil || service.Service.Config == nil {
			continue
		}
		sc := service.Service
		name := strings.TrimSpace(sc.Config.Name)
		if name == "" {
			continue
		}
		for _, info := range service.GetTypeRouters(types.ServerManagerType) {
			if info == nil {
				continue
			}
			if info.GetPath() != serverManageGetMenuPath {
				continue
			}
			cands = append(cands, getMenuCandidate{name: name, sc: sc, info: info})
			break
		}
	}
	if len(cands) == 0 {
		return nil
	}
	authorityName := ""
	if own.manageAuthAuthority != nil {
		authorityName = strings.TrimSpace(own.manageAuthAuthority.name)
	}
	chosen := selectGetMenuCanonicalOwner(cands, authorityName)
	if chosen.sc == nil || chosen.info == nil {
		return nil
	}
	if err := mount.handle(serverManageGetMenuPath, "getmenu:"+chosen.name,
		rest.NewExternalRouterHandler(chosen.sc, chosen.info)); err != nil {
		return err
	}
	logx.Infow("htmlserver_getmenu_canonical_owner",
		logx.Field("service", chosen.name),
		logx.Field("candidates", len(cands)),
	)
	return nil
}

// selectGetMenuCanonicalOwner 在候选中确定性选择 getmenu 规范路径所有者。
// 优先级：server > ManageAuthAuthority 服务名 > 名字典序最小。
func selectGetMenuCanonicalOwner(cands []getMenuCandidate, authorityName string) getMenuCandidate {
	if len(cands) == 0 {
		return getMenuCandidate{}
	}
	byName := make(map[string]getMenuCandidate, len(cands))
	names := make([]string, 0, len(cands))
	for _, c := range cands {
		key := strings.ToLower(strings.TrimSpace(c.name))
		if key == "" {
			continue
		}
		// 同名取首次出现，保持稳定
		if _, ok := byName[key]; !ok {
			byName[key] = c
			names = append(names, key)
		}
	}
	if len(names) == 0 {
		return getMenuCandidate{}
	}
	if c, ok := byName["server"]; ok {
		return c
	}
	auth := strings.ToLower(strings.TrimSpace(authorityName))
	if auth != "" {
		if c, ok := byName[auth]; ok {
			return c
		}
	}
	// 字典序最小
	sort.Strings(names)
	return byName[names[0]]
}

// stripServiceSuffixHandler 将兼容路径 .../{service} 改写为规范路径再交给安全 Handler。
func stripServiceSuffixHandler(serviceName string, next http.Handler) http.Handler {
	suffix := "/" + strings.TrimSpace(serviceName)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if next == nil {
			http.NotFound(w, r)
			return
		}
		pathOnly := r.URL.Path
		if !strings.HasSuffix(pathOnly, suffix) {
			http.NotFound(w, r)
			return
		}
		canonical := strings.TrimSuffix(pathOnly, suffix)
		if canonical == "" {
			http.NotFound(w, r)
			return
		}
		clone := r.Clone(r.Context())
		u := *r.URL
		u.Path = canonical
		u.RawPath = ""
		clone.URL = &u
		if r.URL.RawQuery != "" {
			clone.RequestURI = canonical + "?" + r.URL.RawQuery
		} else {
			clone.RequestURI = canonical
		}
		next.ServeHTTP(w, clone)
	})
}

// spaFallbackHandler 为前端导航提供 index.html 回退；API/静态资源缺失保持 404。
func spaFallbackHandler(dist fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqPath := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
		// /api 与 /swagger 永不由 SPA 处理（含错误方法），避免未挂载 API 被 405 掩盖
		if reqPath != "/" && (strings.HasPrefix(reqPath, "/api/") || reqPath == "/api" ||
			strings.HasPrefix(reqPath, "/swagger/") || reqPath == "/swagger") {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		// 尝试真实静态文件
		rel := strings.TrimPrefix(reqPath, "/")
		if rel == "" || rel == "." {
			rel = "index.html"
		}
		if f, err := dist.Open(rel); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// 带扩展名的缺失资源 → 404（不吞为 SPA）
		base := path.Base(reqPath)
		if strings.Contains(base, ".") && base != "index.html" {
			http.NotFound(w, r)
			return
		}
		// SPA 导航回退
		serveEmbedFile(w, r, dist, "index.html")
	})
}

func serveEmbedFile(w http.ResponseWriter, r *http.Request, fsys fs.FS, name string) {
	f, err := fsys.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// http.ServeContent needs io.ReadSeeker
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		data, err := io.ReadAll(f)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		http.ServeContent(w, r, name, stat.ModTime(), bytes.NewReader(data))
		return
	}
	http.ServeContent(w, r, name, stat.ModTime(), rs)
}

// Handler 返回 Prepare 缓存的 Handler；未 Prepare 时返回 nil。
func (own *HTMLServer) Handler() http.Handler {
	own.lifecycleMu.Lock()
	defer own.lifecycleMu.Unlock()
	return own.handler
}

// startHTTPHandler 供 Start 使用：仅返回已缓存 Handler，绝不调用 Prepare。
func (own *HTMLServer) startHTTPHandler() http.Handler {
	return own.Handler()
}

var qs = &public.QueryService{}

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
	// 只使用 initializeServers 预构建的 Handler；未 Prepare 是编程错误，不得惰性回退。
	handler := own.startHTTPHandler()
	if handler == nil {
		logx.Error("HTMLServer 未 Prepare，拒绝启动")
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

// htmlOpenAPIHandler 仅处理 /api/openapi 聚合文档，不承载 Manage 业务。
// servers 使用请求同源 authority，配合 HTMLServer 上挂载的 Public/Private。
func htmlOpenAPIHandler(service ...*router.ServiceRouter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/openapi" {
			http.NotFound(w, r)
			return
		}
		httpx.OkJson(w, GetOpenApiSameOrigin(r, service...))
	}
}
