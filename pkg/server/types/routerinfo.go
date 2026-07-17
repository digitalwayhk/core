package types

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/utils"

	"github.com/zeromicro/go-zero/core/logx"
)

// IRouterResettable 覆盖对象池默认的通用反射重置。
//
// Reset 在 Router 从池中取出、交给下一次 Parse 之前调用。复杂 Router（例如包含
// 嵌套结构、内部缓存、不可直接清零的指针或自定义资源）应实现本接口，并把所有请求级
// 状态恢复到新实例等价状态。实现后框架不会再执行通用反射重置。
type IRouterResettable interface {
	Reset()
}

// IRouterCleanable 在 Router 归还对象池之前清理请求级状态。
//
// Clean 适合尽早移除 token、用户信息、大块缓冲区和外部资源引用。它不能替代 Reset；
// Clean 负责回收前清理，Reset 负责下一次使用前恢复。实现必须幂等且不能保留异步任务
// 对当前 Router 的引用。
type IRouterCleanable interface {
	Clean()
}
type ApiType string

var (
	PublicType        ApiType = "public"
	PrivateType       ApiType = "private"
	ManageType        ApiType = "manage"
	ServerManagerType ApiType = "servermanager"
)

// RouterInfo 描述并管理 ServiceContext 内的一条路由。
//
// 每个 ServiceContext、每个路由 Path 只应存在一个长期 RouterInfo。它保存稳定的路由
// 元数据、原型实例和路由级运行组件句柄，可被并发请求共享；它不是请求对象，不能保存
// 当前用户、请求参数、trace、响应或其他请求级可变状态。服务注册完成后，Path、Auth、
// Method、ServiceName、PathType 和实例类型等身份字段应视为只读。
//
// RouterInfo.New、ParseNew、JsonNew 和 Exec 使用池化的请求级 IRouter。标准 Router
// 由类型工厂创建，IRouterFactory 可覆盖创建方式；实例从本 RouterInfo 专属的有界池
// 取得，并通过 IRouterResettable/IRouterCleanable 或默认重置逻辑安全复用。
// NewSubscription、ParseSubscription 创建独立 WebSocket 订阅实例；它由 Hub 持有到
// 退订、断线或关闭，最后执行 Clean 并丢弃，绝不能放入请求对象池。
//
// UseCache、Subscribe 和 WebSocket 相关方法是框架公开兼容入口。后续实现应委托给所属
// ServiceContext 的 RouteCacheManager、ServiceEventBridge 和 RouteWebSocketHub，避免
// 在 RouterInfo 内继续累积服务级队列、连接和跨节点状态。Destroy 必须幂等，只关闭
// 本路由持有的资源，不得影响其他服务或其他 RouterInfo。
type RouterInfo struct {
	ID                uint64  // Deprecated: 注册后请使用 GetID；仅为源码兼容保留导出字段。
	Path              string  // Deprecated: 注册后请使用 GetPath；仅为源码兼容保留导出字段。
	Auth              bool    // Deprecated: 注册后请使用 GetAuth；仅为源码兼容保留导出字段。
	Method            string  // Deprecated: 注册后请使用 GetMethod；仅为源码兼容保留导出字段。
	ServiceName       string  // Deprecated: 注册后请使用 GetServiceName；仅为源码兼容保留导出字段。
	PackPath          string  // Deprecated: 注册后请使用 GetPackPath；仅为源码兼容保留导出字段。
	PathType          ApiType // Deprecated: 注册后请使用 GetPathType；仅为源码兼容保留导出字段。
	StructName        string  // Deprecated: 注册后请使用 GetStructName；仅为源码兼容保留导出字段。
	InstanceName      string  // Deprecated: 注册后请使用 GetInstanceName；仅为源码兼容保留导出字段。
	instance          IRouter
	WebSocketWaitTime time.Duration                            //websocket默认通知的循环等待时间 默认:10秒
	Subscriber        map[ObserveState]map[string]*ObserveArgs //订阅者
	useCache          bool                                     //是否使用缓存
	cacheTime         time.Duration                            //缓存时间
	sync.RWMutex
	// Deprecated: RouterInfo 不得保存请求、用户、trace 或响应等请求级状态。
	// 该字段仅为源码兼容保留，框架内部不使用；新代码不得写入。
	TempStore sync.Map
	// 自定义响应处理函数
	ResponseHandlerFunc func(w http.ResponseWriter, r *http.Request, res IResponse) `json:"-"`
	// PoolSize 控制该路由对象池的容量。0 表示使用默认自适应值（基于 GOMAXPROCS）。
	// 高并发路由可适当增大（如 512），低频管理路由可设为 8 减少内存占用。
	// 必须在路由注册完成（Freeze）前设置，之后修改无效。
	PoolSize             int // Deprecated: 注册后请使用 GetPoolSize；仅为源码兼容保留导出字段。
	channelPool          *ChannelPool
	eventRuntime         RouteEventRuntime
	eventCancels         map[ObserveState]map[string]func()
	webSocketHub         *RouteWebSocketHub
	cacheRuntime         RouteCacheRuntime
	externalRateLimit    ExternalRateLimitPolicy
	hasExternalRateLimit bool
	// InternalCallers 是注册期声明的可信内部服务白名单。
	// Deprecated: 注册时请使用 router.WithInternalCallers，读取时请使用 GetInternalCallers。
	InternalCallers []string
	owner           string
	frozen          bool
	frozenMetadata  routerMetadata

	// 🆕 性能统计字段
	stats     *RouterStats `json:"-"`
	statsLock sync.RWMutex
}

type routerMetadata struct {
	id                   uint64
	path                 string
	serviceName          string
	auth                 bool
	method               string
	packPath             string
	pathType             ApiType
	structName           string
	instanceName         string
	instanceType         string
	poolSize             int
	externalRateLimit    ExternalRateLimitPolicy
	hasExternalRateLimit bool
	internalCallers      string
}

// Freeze 在路由完成注册后冻结身份元数据。重复冻结仅在所有者和元数据一致时幂等。
func (own *RouterInfo) Freeze(owner string) {
	own.Lock()
	defer own.Unlock()
	if own.frozen {
		own.assertMetadataFrozenLocked()
		if own.owner != owner {
			panic("router metadata owner conflict")
		}
		return
	}
	own.owner = owner
	own.InternalCallers = normalizeInternalCallers(own.InternalCallers)
	own.frozenMetadata = own.currentMetadataLocked()
	own.frozen = true
}

func (own *RouterInfo) currentMetadataLocked() routerMetadata {
	instanceType := ""
	if own.instance != nil {
		instanceType = reflect.TypeOf(own.instance).String()
	}
	return routerMetadata{
		id:                   own.ID,
		path:                 own.Path,
		serviceName:          own.ServiceName,
		auth:                 own.Auth,
		method:               own.Method,
		packPath:             own.PackPath,
		pathType:             own.PathType,
		structName:           own.StructName,
		instanceName:         own.InstanceName,
		instanceType:         instanceType,
		poolSize:             own.PoolSize,
		externalRateLimit:    own.externalRateLimit,
		hasExternalRateLimit: own.hasExternalRateLimit,
		internalCallers:      strings.Join(own.InternalCallers, "\x00"),
	}
}

func normalizeInternalCallers(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func (own *RouterInfo) assertMetadataFrozenLocked() {
	if own.frozen && own.currentMetadataLocked() != own.frozenMetadata {
		panic("router metadata changed after registration")
	}
}

func (own *RouterInfo) assertMetadataFrozen() {
	own.RLock()
	defer own.RUnlock()
	own.assertMetadataFrozenLocked()
}

// PrepareRegistration 校验 RouterInfo 的注册所有者。
// 返回 true 表示元数据尚未冻结，调用方可以配置并完成首次注册；返回 false 表示同一
// owner 正在复用已冻结元数据，调用方只能读取，不能再次配置或绑定运行组件。
func (own *RouterInfo) PrepareRegistration(owner string) bool {
	own.RLock()
	defer own.RUnlock()
	own.assertMetadataFrozenLocked()
	if !own.frozen {
		return true
	}
	if own.owner != owner {
		panic("router metadata owner conflict")
	}
	return false
}

func (own *RouterInfo) New() IRouter {
	return own.getNew()
}
func (own *RouterInfo) ParseNew(instance interface{}) (IRouter, error) {
	item := own.New()
	value, err := json.Marshal(instance)
	if err != nil {
		logx.Error(err)
		own.putRouter(item)
		return nil, err
	}
	err = json.Unmarshal(value, item)
	if err != nil {
		logx.Error(err)
		own.putRouter(item)
		return nil, err
	}
	return item, err
}
func (own *RouterInfo) JsonNew(txt string) (IRouter, error) {
	item := own.New()
	err := json.Unmarshal(utils.String2Bytes(txt), item)
	if err != nil {
		logx.Error(err)
		own.putRouter(item)
		return nil, err
	}
	return item, nil
}

// NewSubscription 创建一个不进入请求对象池的 WebSocket 订阅实例。
// 该实例由 RouteWebSocketHub 持有到退订、断线或 Hub 关闭。
func (own *RouterInfo) NewSubscription() IRouter {
	return own.newRouterInstance()
}

// ParseSubscription 创建并解析一个独立 WebSocket 订阅实例。
func (own *RouterInfo) ParseSubscription(instance interface{}) (IRouter, error) {
	item := own.NewSubscription()
	if item == nil {
		return nil, errors.New("create websocket subscription router failed")
	}
	value, err := json.Marshal(instance)
	if err != nil {
		own.releaseSubscription(item)
		return nil, err
	}
	if err := json.Unmarshal(value, item); err != nil {
		own.releaseSubscription(item)
		return nil, err
	}
	return item, nil
}

// ReleaseSubscription 清理尚未交给 Hub 或已从 Hub 移除的订阅实例。
func (own *RouterInfo) ReleaseSubscription(router IRouter) {
	own.releaseSubscription(router)
}
func (own *RouterInfo) GetInstance() interface{} {
	return own.instance
}
func (own *RouterInfo) SetInstance(instance IRouter) {
	own.Lock()
	defer own.Unlock()
	if own.frozen {
		panic("router instance cannot change after registration")
	}
	own.instance = instance
}

// SetEventBridge 把路由观察事件绑定到所属服务。同一服务关闭并重建时允许替换运行时，
// 但不同所有者不能接管已经冻结的 RouterInfo。
func (own *RouterInfo) SetEventBridge(owner string, runtime RouteEventRuntime) {
	own.Lock()
	defer own.Unlock()
	own.assertMetadataFrozenLocked()
	if own.frozen && own.owner != owner {
		panic("router event bridge owner conflict")
	}
	for _, byAddress := range own.eventCancels {
		for _, cancel := range byAddress {
			if cancel != nil {
				cancel()
			}
		}
	}
	own.eventCancels = nil
	own.eventRuntime = runtime
	for _, byAddress := range own.Subscriber {
		for _, observer := range byAddress {
			if err := own.subscribeEventLocked(observer); err != nil {
				panic("router event bridge subscription failed")
			}
		}
	}
}

// SetWebSocketHub 绑定所属 ServiceContext 的 WebSocket 运行时。同名服务重建时可以
// 替换已关闭的 Hub，不允许其他服务接管已经冻结的 RouterInfo。
func (own *RouterInfo) SetWebSocketHub(owner string, hub *RouteWebSocketHub) {
	own.Lock()
	defer own.Unlock()
	own.assertMetadataFrozenLocked()
	if own.frozen && own.owner != owner {
		panic("router websocket hub owner conflict")
	}
	own.webSocketHub = hub
}

// SetCacheManager 绑定所属 ServiceContext 的缓存运行时，并恢复注册阶段声明的 UseCache。
func (own *RouterInfo) SetCacheManager(owner string, runtime RouteCacheRuntime) {
	own.Lock()
	defer own.Unlock()
	own.assertMetadataFrozenLocked()
	if own.frozen && own.owner != owner {
		panic("router cache manager owner conflict")
	}
	own.cacheRuntime = runtime
	if runtime != nil && own.useCache {
		if err := runtime.EnableRoute(own.Path, own.cacheTime); err != nil {
			panic("router cache enable failed")
		}
	}
}
func (own *RouterInfo) GetPath() string {
	own.RLock()
	defer own.RUnlock()
	own.assertMetadataFrozenLocked()
	return own.Path
}
func (own *RouterInfo) GetServiceName() string {
	own.RLock()
	defer own.RUnlock()
	own.assertMetadataFrozenLocked()
	return own.ServiceName
}

// GetInternalCallers 返回注册期冻结的可信内部服务白名单副本。
func (own *RouterInfo) GetInternalCallers() []string {
	own.RLock()
	defer own.RUnlock()
	own.assertMetadataFrozenLocked()
	return append([]string(nil), own.InternalCallers...)
}

// GetID 返回路由注册期确定的稳定 ID。
func (own *RouterInfo) GetID() uint64 {
	own.RLock()
	defer own.RUnlock()
	own.assertMetadataFrozenLocked()
	return own.ID
}

// GetAuth 返回路由注册期确定的认证要求。
func (own *RouterInfo) GetAuth() bool {
	own.RLock()
	defer own.RUnlock()
	own.assertMetadataFrozenLocked()
	return own.Auth
}

// GetMethod 返回路由注册期确定的 HTTP 方法。
func (own *RouterInfo) GetMethod() string {
	own.RLock()
	defer own.RUnlock()
	own.assertMetadataFrozenLocked()
	return own.Method
}

// GetPackPath 返回路由实现的包路径。
func (own *RouterInfo) GetPackPath() string {
	own.RLock()
	defer own.RUnlock()
	own.assertMetadataFrozenLocked()
	return own.PackPath
}

// GetPathType 返回路由注册类型。
func (own *RouterInfo) GetPathType() ApiType {
	own.RLock()
	defer own.RUnlock()
	own.assertMetadataFrozenLocked()
	return own.PathType
}

// GetStructName 返回路由实例的结构名称。
func (own *RouterInfo) GetStructName() string {
	own.RLock()
	defer own.RUnlock()
	own.assertMetadataFrozenLocked()
	return own.StructName
}

// GetInstanceName 返回用于生成默认路径的实例名称。
func (own *RouterInfo) GetInstanceName() string {
	own.RLock()
	defer own.RUnlock()
	own.assertMetadataFrozenLocked()
	return own.InstanceName
}

// GetPoolSize 返回路由注册期确定的对象池容量配置。
func (own *RouterInfo) GetPoolSize() int {
	own.RLock()
	defer own.RUnlock()
	own.assertMetadataFrozenLocked()
	return own.PoolSize
}

//	func (own *RouterInfo) limit(ip string, userid uint) error {
//		if config.INITSERVER {
//			return nil
//		}
//		own.Lock()
//		defer own.Unlock()
//		if own.iplasttime == nil {
//			own.iplasttime = make(map[string]time.Time)
//		}
//		if lasttiem, ok := own.iplasttime[ip]; ok {
//			if time.Since(lasttiem) < own.SpeedLimit {
//				return errors.New("ip too many request")
//			}
//		} else {
//			own.iplasttime[ip] = time.Now()
//		}
//		if own.LimitType == 1 {
//			if own.userlasttime == nil {
//				own.userlasttime = make(map[uint]time.Time)
//			}
//			if lasttiem, ok := own.userlasttime[userid]; ok {
//				if time.Since(lasttiem) < own.SpeedLimit {
//					return errors.New("user too many request")
//				}
//			} else {
//				own.userlasttime[userid] = time.Now()
//			}
//		}
//		return nil
//	}
func (own *RouterInfo) Exec(req IRequest) (resp IResponse) {
	own.assertMetadataFrozen()
	if err := own.AuthorizeInternalCaller(req); err != nil {
		return req.NewResponse(nil, err)
	}
	api := own.New()
	delegated := false
	// 🔧 使用 defer 确保对象回收，并通过具名返回值在 panic 时返回错误响应
	defer func() {
		// Parse 失败或 panic 时 ExecDo 尚未接管实例，由 Exec 归还。
		if !delegated {
			own.putRouter(api)
		}
		if config.IsServerInitializing() {
			return
		}

		if err := recover(); err != nil {
			logx.Errorw("router_execution_panicked",
				logx.Field("service", own.ServiceName),
				logx.Field("route", own.Path),
				logx.Field("trace_id", req.GetTraceId()),
				logx.Field("error", err),
			)
			if resp == nil {
				cause := fmt.Errorf("%v", err)
				panicErr := NewTypeErrorWithCause(own.ServiceName, own.Path, "panic", cause.Error(), 500, cause)
				resp = req.NewResponse(nil, panicErr)
			}
		}
	}()
	err := api.Parse(req)
	if err != nil {
		msg := fmt.Sprintf("参数解析异常:%s", err)
		err = NewTypeErrorWithCause(own.ServiceName, own.Path, "parse", msg, 600, err)
		return req.NewResponse(nil, err)
	}
	delegated = true
	return own.ExecDo(api, req)
}

// 🔧 修改 ExecDo 方法，添加统计
func (own *RouterInfo) ExecDo(api IRouter, req IRequest) (resp IResponse) {
	defer func() {
		own.putRouter(api)
	}()
	own.assertMetadataFrozen()
	if err := own.AuthorizeInternalCaller(req); err != nil {
		return req.NewResponse(nil, err)
	}
	// 🆕 记录请求开始
	recordEnd := own.recordRequestStart()
	startTime := time.Now()

	defer func() {
		if config.IsServerInitializing() {
			return
		}
		if err := recover(); err != nil {
			logx.Errorw("router_do_panicked",
				logx.Field("service", own.ServiceName),
				logx.Field("route", own.Path),
				logx.Field("trace_id", req.GetTraceId()),
				logx.Field("error", err),
				logx.Field("stack", string(debug.Stack())),
			)

			// 🆕 记录异常
			cause := fmt.Errorf("%v", err)
			own.recordRequestEnd(startTime, cause)
			panicErr := NewTypeErrorWithCause(own.ServiceName, own.Path, "panic", cause.Error(), 500, cause)
			resp = req.NewResponse(nil, panicErr)
		} else {
			// 🆕 正常结束
			recordEnd()
		}
	}()

	err := api.Validation(req)
	if err != nil {
		msg := fmt.Sprintf("业务验证异常:%s", err)
		err = NewTypeErrorWithCause(own.ServiceName, own.Path, "validation", msg, 700, err)
		return req.NewResponse(nil, err)
	}

	if own.useCache {
		if cache := own.getCache(api); cache != nil {
			// 🆕 记录缓存命中
			own.recordCacheHit()

			resp = req.NewResponse(cache.data, nil)
			go own.responseNotify(snapshotNotifyValue(api), req.GetTraceId(), snapshotNotifyValue(resp))
			return resp
		} else {
			// 🆕 记录缓存未命中
			own.recordCacheMiss()
		}
	}

	go own.requestNotify(snapshotNotifyValue(api), req.GetTraceId())
	var data interface{}
	usedTakeRuntime := false
	if own.useCache {
		data, err, usedTakeRuntime = own.takeCache(api, func() (interface{}, error) {
			return api.Do(req)
		})
	}
	if !usedTakeRuntime {
		data, err = api.Do(req)
	}
	if err != nil {
		msg := fmt.Sprintf("调用执行异常:%s", err)
		err = NewTypeErrorWithCause(own.ServiceName, own.Path, "do", msg, 800, err)
	} else {
		if own.useCache && !usedTakeRuntime && data != nil {
			own.setCache(api, data)
		}
	}

	resp = req.NewResponse(data, err)
	if err != nil {
		go own.errorNotify(snapshotNotifyValue(api), req.GetTraceId(), snapshotNotifyValue(resp))
	} else {
		go own.responseNotify(snapshotNotifyValue(api), req.GetTraceId(), snapshotNotifyValue(resp))
	}
	return resp
}

func (own *RouterInfo) takeCache(api IRouter, loader func() (interface{}, error)) (interface{}, error, bool) {
	own.assertMetadataFrozen()
	own.RLock()
	runtime := own.cacheRuntime
	path := own.Path
	ttl := own.cacheTime
	own.RUnlock()
	takeRuntime, ok := runtime.(RouteCacheTakeRuntime)
	if !ok {
		return nil, nil, false
	}
	value, err := takeRuntime.TakeBestEffort(path, api, ttl, loader)
	return value, err, true
}

func (own *RouterInfo) Subscribe(ob *ObserveArgs) error {
	if ob == nil {
		return errors.New("subscriber is nil")
	}
	own.Lock()
	defer own.Unlock()
	own.assertMetadataFrozenLocked()
	if own.Subscriber == nil {
		own.Subscriber = make(map[ObserveState]map[string]*ObserveArgs, 3)
	}
	if own.Subscriber[ob.State] == nil {
		own.Subscriber[ob.State] = make(map[string]*ObserveArgs, 0)
	}
	if _, ok := own.Subscriber[ob.State][ob.ReceiveService]; ok {
		return nil //errors.New("subscriber already exists")
	}
	copied := *ob
	copied.ServiceName = own.ServiceName
	if err := own.subscribeEventLocked(&copied); err != nil {
		return err
	}
	own.Subscriber[ob.State][ob.ReceiveService] = &copied
	return nil
}

func (own *RouterInfo) subscribeEventLocked(observer *ObserveArgs) error {
	if own.eventRuntime == nil || observer == nil {
		return nil
	}
	cancel, err := own.eventRuntime.Subscribe(own.observeEventType(observer.State), func(env *event.Envelope) {
		args := &NotifyArgs{}
		if env == nil || json.Unmarshal(env.Data, args) != nil {
			return
		}
		args.ReceiveService = observer.ReceiveService
		_ = observer.Notify(args)
	})
	if err != nil {
		return err
	}
	if own.eventCancels == nil {
		own.eventCancels = make(map[ObserveState]map[string]func(), 3)
	}
	if own.eventCancels[observer.State] == nil {
		own.eventCancels[observer.State] = make(map[string]func())
	}
	own.eventCancels[observer.State][observer.ReceiveService] = cancel
	return nil
}
func (own *RouterInfo) UnSubscribe(ob *ObserveArgs) error {
	if ob == nil {
		return errors.New("subscriber is nil")
	}
	own.Lock()
	if own.Subscriber[ob.State] == nil {
		own.Unlock()
		return errors.New("subscriber not exists")
	}
	delete(own.Subscriber[ob.State], ob.ReceiveService)
	var cancel func()
	if own.eventCancels[ob.State] != nil {
		cancel = own.eventCancels[ob.State][ob.ReceiveService]
		delete(own.eventCancels[ob.State], ob.ReceiveService)
	}
	own.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (own *RouterInfo) observeEventType(state ObserveState) string {
	return "router.observe:" + own.ServiceName + ":" + own.Path + ":" + strconv.Itoa(int(state))
}
func snapshotNotifyValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return json.RawMessage(data)
}

func (own *RouterInfo) subscriberSnapshot(state ObserveState) []*ObserveArgs {
	own.RLock()
	defer own.RUnlock()
	own.assertMetadataFrozenLocked()
	source := own.Subscriber[state]
	items := make([]*ObserveArgs, 0, len(source))
	for _, item := range source {
		if item == nil {
			continue
		}
		copied := *item
		copied.ServiceName = own.ServiceName
		items = append(items, &copied)
	}
	return items
}

func (own *RouterInfo) requestNotify(api interface{}, traceid string) {
	if own.publishObservation(ObserveRequest, api, traceid, nil) {
		return
	}
	items := own.subscriberSnapshot(ObserveRequest)
	for _, item := range items {
		na := item.NewNotifyArgsSnapshot(api, nil)
		na.TraceID = traceid
		err := item.Notify(na)
		if err != nil {
			logx.Error(err, item)
		}
	}
}
func (own *RouterInfo) responseNotify(api interface{}, traceid string, resp interface{}) {
	if own.publishObservation(ObserveResponse, api, traceid, resp) {
		return
	}
	items := own.subscriberSnapshot(ObserveResponse)
	for _, item := range items {
		na := item.NewNotifyArgsSnapshot(api, resp)
		na.TraceID = traceid
		err := item.Notify(na)
		if err != nil {
			logx.Error(err, item)
		}
	}
}
func (own *RouterInfo) errorNotify(api interface{}, traceid string, resp interface{}) {
	if own.publishObservation(ObserveError, api, traceid, resp) {
		return
	}
	items := own.subscriberSnapshot(ObserveError)
	for _, item := range items {
		na := item.NewNotifyArgsSnapshot(api, resp)
		na.TraceID = traceid
		err := item.Notify(na)
		if err != nil {
			logx.Error(err, item)
		}
	}
}

func (own *RouterInfo) publishObservation(state ObserveState, api interface{}, traceID string, response interface{}) bool {
	own.assertMetadataFrozen()
	own.RLock()
	runtime := own.eventRuntime
	eventType := own.observeEventType(state)
	serviceName := own.ServiceName
	path := own.Path
	own.RUnlock()
	if runtime == nil {
		return false
	}
	env := event.NewEnvelope(serviceName, eventType, nil)
	env.Subject = path
	err := runtime.Publish(context.Background(), event.PublishRequest{
		Class:    event.ObserverDelivery,
		Envelope: env,
		BuildData: func() ([]byte, error) {
			return json.Marshal(&NotifyArgs{
				TraceID:     traceID,
				SendService: serviceName,
				Topic:       path,
				State:       state,
				Instance:    api,
				Response:    response,
			})
		},
	})
	if err != nil && !errors.Is(err, event.ErrServiceEventBridgeClosed) {
		logx.Errorw("router_observer_publish_failed",
			logx.Field("service", serviceName),
			logx.Field("route", path),
			logx.Field("state", state),
			logx.Field("error", err),
		)
	}
	return true
}

type cacheObject struct {
	data interface{}
}

func (own *RouterInfo) UseCache(cacheTime time.Duration) {
	own.assertMetadataFrozen()
	own.Lock()
	own.useCache = true
	own.cacheTime = cacheTime
	if cacheTime <= 0 {
		own.cacheTime = time.Second * 10 //默认缓存10秒
	}
	runtime := own.cacheRuntime
	path := own.Path
	own.Unlock()
	if runtime != nil {
		if err := runtime.EnableRoute(path, own.cacheTime); err != nil {
			logx.Errorw("route_cache_enable_failed",
				logx.Field("service", own.ServiceName),
				logx.Field("route", path),
				logx.Field("error", err),
			)
		}
	}
}
func (own *RouterInfo) getCache(api IRouter) *cacheObject {
	own.assertMetadataFrozen()
	own.RLock()
	runtime := own.cacheRuntime
	path := own.Path
	own.RUnlock()
	if runtime != nil {
		value, ok, err := runtime.Get(path, api)
		if err != nil || !ok {
			return nil
		}
		return &cacheObject{data: value}
	}
	return nil
}
func (own *RouterInfo) setCache(api IRouter, value interface{}) {
	own.assertMetadataFrozen()
	own.RLock()
	runtime := own.cacheRuntime
	path := own.Path
	ttl := own.cacheTime
	own.RUnlock()
	if runtime != nil {
		if err := runtime.Set(path, api, value, ttl); err != nil {
			logx.Errorw("route_cache_set_failed",
				logx.Field("service", own.ServiceName),
				logx.Field("route", path),
				logx.Field("error", err),
			)
		}
	}
}
func (own *RouterInfo) FailureCache(api IRouter) {
	own.assertMetadataFrozen()
	own.RLock()
	runtime := own.cacheRuntime
	path := own.Path
	own.RUnlock()
	if runtime != nil {
		var err error
		if api == nil {
			err = runtime.DeleteRoute(path)
		} else {
			err = runtime.Delete(path, api)
		}
		if err != nil {
			logx.Errorw("route_cache_delete_failed",
				logx.Field("service", own.ServiceName),
				logx.Field("route", path),
				logx.Field("error", err),
			)
		}
	}
}
func getApiHash(api IRouter) uint64 {
	if hk, ok := api.(IRouterHashKey); ok {
		return hk.GetHashKey()
	}
	key := ""
	utils.ForEach(api, func(name string, value interface{}) {
		key += utils.ConvertToString(value)
	})
	return utils.HashCode64(key)
}
func (own *RouterInfo) GetWebSocketIRouter() []IRouter {
	own.RLock()
	hub := own.webSocketHub
	own.RUnlock()
	if hub != nil {
		return hub.Routers(own)
	}
	return nil
}

func (own *RouterInfo) UnRegisterWebSocketClient(router IRouter, client IWebSocket) uint64 {
	if router == nil || client == nil {
		return 0
	}
	hash := getApiHash(router)
	own.UnRegisterWebSocketHash(hash, client)
	return hash
}

func (own *RouterInfo) Destroy() {
	own.assertMetadataFrozen()
	own.RLock()
	hub := own.webSocketHub
	own.RUnlock()
	if hub != nil {
		hub.RemoveRoute(own)
	}
	// 🆕 先关闭统计系统
	own.closeStats()

	// 清理WebSocket连接
	own.CleanupDeadConnections()

	logx.Debugw("router_info_destroyed",
		logx.Field("service", own.ServiceName),
		logx.Field("route", own.Path),
	)
}

func StartPeriodicCleanup() {
}

// StopPeriodicCleanup 为旧调用方保留。周期清理已由每服务 RouteWebSocketHub 接管。
func StopPeriodicCleanup(_ context.Context) error {
	return nil
}
