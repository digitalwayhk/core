package types

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"runtime/debug"
	"strconv"
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
// RouterInfo.New、ParseNew、JsonNew 和 Exec 使用独立的请求级 IRouter。标准 Router
// 由类型工厂创建，IRouterFactory 可覆盖创建方式；实例从本 RouterInfo 专属的有界池
// 取得，并通过 IRouterResettable/IRouterCleanable 或默认重置逻辑安全复用。
//
// UseCache、Subscribe 和 WebSocket 相关方法是框架公开兼容入口。后续实现应委托给所属
// ServiceContext 的 RouteCacheManager、ServiceEventBridge 和 RouteWebSocketHub，避免
// 在 RouterInfo 内继续累积服务级队列、连接和跨节点状态。Destroy 必须幂等，只关闭
// 本路由持有的资源，不得影响其他服务或其他 RouterInfo。
type RouterInfo struct {
	ID                uint64
	Path              string
	Auth              bool
	Method            string
	ServiceName       string
	PackPath          string //包路径
	PathType          ApiType
	StructName        string
	InstanceName      string
	instance          IRouter
	WebSocketWaitTime time.Duration                            //websocket默认通知的循环等待时间 默认:10秒
	Subscriber        map[ObserveState]map[string]*ObserveArgs //订阅者
	rCache            sync.Map                                 //路由结果缓存,key:api hash,value:result
	useCache          bool                                     //是否使用缓存
	cacheTime         time.Duration                            //缓存时间
	rArgs             map[uint64]IRouter                       //路由参数
	rHashClients      map[uint64]int                           // per-hash client count for accurate unregister detection
	//rWebSocketClient  map[uint64]map[IWebSocket]IRequest       //websocket客户端
	// 🔧 使用分片替代单一 map
	rWebSocketShards [shardCount]*websocketShard // 替代 rWebSocketClient
	//webSocketHandler  bool                                     //websocket代理处理是否运行
	sync.RWMutex
	once          sync.Once
	TempStore     sync.Map
	websocketlock sync.RWMutex
	// 自定义响应处理函数
	ResponseHandlerFunc func(w http.ResponseWriter, r *http.Request, res IResponse) `json:"-"`
	channelPool         *ChannelPool
	eventRuntime        RouteEventRuntime
	eventCancels        map[ObserveState]map[string]func()
	owner               string
	frozen              bool
	frozenMetadata      routerMetadata

	// 🆕 性能统计字段
	stats     *RouterStats `json:"-"`
	statsLock sync.RWMutex
}

type routerMetadata struct {
	path         string
	serviceName  string
	auth         bool
	method       string
	pathType     ApiType
	structName   string
	instanceName string
	instanceType string
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
	own.frozenMetadata = own.currentMetadataLocked()
	own.frozen = true
}

func (own *RouterInfo) currentMetadataLocked() routerMetadata {
	instanceType := ""
	if own.instance != nil {
		instanceType = reflect.TypeOf(own.instance).String()
	}
	return routerMetadata{
		path:         own.Path,
		serviceName:  own.ServiceName,
		auth:         own.Auth,
		method:       own.Method,
		pathType:     own.PathType,
		structName:   own.StructName,
		instanceName: own.InstanceName,
		instanceType: instanceType,
	}
}

func (own *RouterInfo) assertMetadataFrozenLocked() {
	if own.frozen && own.currentMetadataLocked() != own.frozenMetadata {
		panic("router metadata changed after registration")
	}
}

func (own *RouterInfo) New() IRouter {
	return own.getNew()
}
func (own *RouterInfo) ParseNew(instance interface{}) (IRouter, error) {
	item := own.New()
	value, err := json.Marshal(instance)
	if err != nil {
		logx.Error(err)
		return nil, err
	}
	err = json.Unmarshal(value, item)
	if err != nil {
		logx.Error(err)
		return nil, err
	}
	return item, err
}
func (own *RouterInfo) JsonNew(txt string) (IRouter, error) {
	item := own.New()
	err := json.Unmarshal(utils.String2Bytes(txt), item)
	if err != nil {
		logx.Error(err)
		return nil, err
	}
	return item, nil
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
	api := own.New()
	// 🔧 使用 defer 确保对象回收，并通过具名返回值在 panic 时返回错误响应
	defer func() {
		if config.IsServerInitializing() {
			return
		}

		// 🔧 回收对象到池
		own.putRouter(api)

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
	return own.ExecDo(api, req)
}

// 🔧 修改 ExecDo 方法，添加统计
func (own *RouterInfo) ExecDo(api IRouter, req IRequest) (resp IResponse) {
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
	data, err := api.Do(req)
	if err != nil {
		msg := fmt.Sprintf("调用执行异常:%s", err)
		err = NewTypeErrorWithCause(own.ServiceName, own.Path, "do", msg, 800, err)
	} else {
		if own.useCache && data != nil {
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

func (own *RouterInfo) Subscribe(ob *ObserveArgs) error {
	if ob == nil {
		return errors.New("subscriber is nil")
	}
	own.Lock()
	defer own.Unlock()
	if own.Subscriber == nil {
		own.Subscriber = make(map[ObserveState]map[string]*ObserveArgs, 3)
	}
	if own.Subscriber[ob.State] == nil {
		own.Subscriber[ob.State] = make(map[string]*ObserveArgs, 0)
	}
	if _, ok := own.Subscriber[ob.State][ob.OwnAddress]; ok {
		return nil //errors.New("subscriber already exists")
	}
	copied := *ob
	copied.ServiceName = own.ServiceName
	if err := own.subscribeEventLocked(&copied); err != nil {
		return err
	}
	own.Subscriber[ob.State][ob.OwnAddress] = &copied
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
		args.ReceiveAddress = observer.OwnAddress
		args.ReceiveProt = observer.OwnProt
		args.ReceiveSocketProt = observer.OwnSocketProt
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
	own.eventCancels[observer.State][observer.OwnAddress] = cancel
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
	delete(own.Subscriber[ob.State], ob.OwnAddress)
	var cancel func()
	if own.eventCancels[ob.State] != nil {
		cancel = own.eventCancels[ob.State][ob.OwnAddress]
		delete(own.eventCancels[ob.State], ob.OwnAddress)
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
	updateCacheTime time.Time   //更新缓存时间
	data            interface{} //缓存数据
}

func (own *RouterInfo) UseCache(cacheTime time.Duration) {
	own.useCache = true
	own.cacheTime = cacheTime
	if cacheTime <= 0 {
		own.cacheTime = time.Second * 10 //默认缓存10秒
	}
	own.rCache = sync.Map{}
}
func (own *RouterInfo) getCache(api IRouter) *cacheObject {
	key := getApiHash(api)
	if value, ok := own.rCache.Load(key); ok {
		obj := value.(*cacheObject)
		if obj.updateCacheTime.Add(own.cacheTime).After(time.Now()) {
			return obj
		}
		//缓存过期
		own.rCache.Delete(key)
		return nil
	}
	return nil
}
func (own *RouterInfo) setCache(api IRouter, value interface{}) {
	key := getApiHash(api)
	obj := own.getCache(api)
	if obj == nil {
		obj = &cacheObject{
			updateCacheTime: time.Now(),
			data:            value,
		}
	} else {
		obj.updateCacheTime = time.Now()
		obj.data = value
	}
	own.rCache.Store(key, obj)
}
func (own *RouterInfo) FailureCache(api IRouter) {
	if api == nil {
		own.rCache.Range(func(key, value interface{}) bool {
			own.rCache.Delete(key)
			return true
		})
		return
	}
	key := getApiHash(api)
	own.rCache.Delete(key)
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
	items := make([]IRouter, 0)
	for _, r := range own.rArgs {
		items = append(items, r)
	}
	return items
}

// 注册websocket的订阅，并返回订阅的event号
// func (own *RouterInfo) RegisterWebSocketClient(router IRouter, client IWebSocket, req IRequest) uint64 {
// 	if router == nil || client == nil || req == nil {
// 		return 0
// 	}
// 	own.ensureWebSocketInit()

// 	// 🔧 在锁外声明需要的变量
// 	var needRegister bool
// 	var hash uint64

// 	// 🔧 在锁内只做数据操作
// 	func() {
// 		own.websocketlock.Lock()
// 		defer own.websocketlock.Unlock()

// 		// 🔧 初始化检查
// 		if own.rArgs == nil {
// 			own.rArgs = make(map[uint64]IRouter)
// 		}
// 		if own.rWebSocketClient == nil {
// 			own.rWebSocketClient = make(map[uint64]map[IWebSocket]IRequest)
// 		}

// 		// 🔧 处理私有类型
// 		if own.PathType == PrivateType {
// 			id, _ := req.GetUser()
// 			utils.SetPropertyValue(router, "userid", id)
// 		}

// 		hash = getApiHash(router)

// 		// 🔧 安全地注册路由
// 		if _, ok := own.rArgs[hash]; !ok {
// 			own.rArgs[hash] = router
// 		}

// 		// 🔧 安全地注册客户端
// 		if _, ok := own.rWebSocketClient[hash]; !ok {
// 			own.rWebSocketClient[hash] = make(map[IWebSocket]IRequest)
// 			needRegister = true
// 		}
// 		own.rWebSocketClient[hash][client] = req
// 		// 🆕 记录连接建立
// 		own.recordWebSocketConnect(hash)
// 	}()

// 	// 🔧 在锁外调用外部方法
// 	if needRegister {
// 		if iwsr, ok := router.(IWebSocketRouter); ok {
// 			func() {
// 				defer func() {
// 					if err := recover(); err != nil {
// 						logx.Error("RegisterWebSocket panic:", err)
// 					}
// 				}()
// 				iwsr.RegisterWebSocket(client, req)
// 			}()
// 		}
// 	}

//		return hash
//	}
func (own *RouterInfo) UnRegisterWebSocketClient(router IRouter, client IWebSocket) uint64 {
	if router == nil || client == nil {
		return 0
	}
	hash := getApiHash(router)
	own.UnRegisterWebSocketHash(hash, client)
	return hash
}

// func (own *RouterInfo) UnRegisterWebSocketHash(hash uint64, client IWebSocket) {
// 	if client == nil {
// 		return
// 	}

// 	// 🔧 在锁外声明需要调用的变量
// 	var needUnregister bool
// 	var api IRouter
// 	var req IRequest

// 	// 🔧 在锁内只做数据操作
// 	func() {
// 		own.websocketlock.Lock()
// 		defer own.websocketlock.Unlock()

// 		// 🔧 安全检查
// 		if own.rWebSocketClient == nil || own.rArgs == nil {
// 			return
// 		}

// 		// 🔧 获取请求对象和API
// 		if clients, ok := own.rWebSocketClient[hash]; ok {
// 			req = clients[client]

// 			delete(clients, client)

// 			// 🔧 如果没有客户端了，准备清理资源
// 			if len(clients) == 0 {
// 				api = own.rArgs[hash]
// 				if api != nil {
// 					needUnregister = true
// 				}
// 				delete(own.rWebSocketClient, hash)
// 				delete(own.rArgs, hash)
// 			}
// 		}

// 		// 🔧 检查是否需要关闭处理器
// 		if len(own.rArgs) == 0 {
// 			own.webSocketHandler = false
// 		}

// 		own.recordWebSocketDisconnect(hash)
// 	}()

// 	// 🔧 在锁外调用外部接口
// 	if needUnregister && api != nil {
// 		if iwsr, ok := api.(IWebSocketRouter); ok {
// 			func() {
// 				defer func() {
// 					if err := recover(); err != nil {
// 						logx.Error("UnRegisterWebSocket panic:", err)
// 					}
// 				}()
// 				iwsr.UnRegisterWebSocket(client, req)
// 			}()
// 		}
// 	}
// }

// 🔧 添加工作池
type noticeJob struct {
	hash    uint64
	api     IRouter
	message interface{}
	iwsr    IWebSocketRouterNotice
	router  *RouterInfo
}

var (
	noticeJobChan = make(chan *noticeJob, 1000) // 缓冲通道
	workerOnce    sync.Once
)

// func (own *RouterInfo) NoticeWebSocket(message interface{}) {
// 	if iwsr, ok := own.instance.(IWebSocketRouterNotice); ok {
// 		// 🔧 确保工作池启动
// 		workerOnce.Do(func() {
// 			own.startNoticeWorkers()
// 		})

// 		// 🔧 快速收集并提交任务
// 		hashApis := own.collectHashApis()
// 		for hash, api := range hashApis {
// 			job := &noticeJob{
// 				hash:    hash,
// 				api:     api,
// 				message: message,
// 				iwsr:    iwsr,
// 				router:  own,
// 			}

// 			// 🔧 非阻塞提交，如果队列满了就丢弃
// 			select {
// 			case noticeJobChan <- job:
// 			default:
// 				logx.Errorf("Notice job queue full, dropping job for hash:%d", hash)
// 			}
// 		}
// 	}
// }

// 🔧 启动工作协程池
// func (own *RouterInfo) startNoticeWorkers() {
// 	const workerCount = 10 // 可配置

// 	for i := 0; i < workerCount; i++ {
// 		go own.noticeWorker(i)
// 	}
// 	logx.Infof("Started %d notice workers", workerCount)
// }

// 🔧 工作协程
func (own *RouterInfo) noticeWorker(workerID int) {
	for job := range noticeJobChan {
		func() {
			defer func() {
				if err := recover(); err != nil {
					logx.Errorf("Worker %d panic processing job for hash:%d, error:%v",
						workerID, job.hash, err)
				}
			}()

			// 🔧 带超时的处理
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			done := make(chan bool, 1)
			var ok bool
			var ndata interface{}

			go func() {
				defer func() {
					if err := recover(); err != nil {
						logx.Errorf("NoticeFiltersRouter panic in worker %d: %v", workerID, err)
					}
					done <- true
				}()
				ok, ndata = job.iwsr.NoticeFiltersRouter(job.message, job.api)
			}()

			select {
			case <-done:
				if ok {
					job.router.sendToHashClients(job.hash, job.message, ndata)
				}
			case <-ctx.Done():
				logx.Errorf("Worker %d: NoticeFiltersRouter timeout for hash:%d", workerID, job.hash)
			}
		}()
	}
}

// 🔧 快速收集hash和api映射
// func (own *RouterInfo) collectHashApis() map[uint64]IRouter {
// 	own.websocketlock.RLock()
// 	defer own.websocketlock.RUnlock()

// 	if len(own.rArgs) == 0 {
// 		return nil
// 	}

// 	// 复制映射，避免在异步处理中出现并发问题
// 	hashApis := make(map[uint64]IRouter, len(own.rArgs))
// 	for hash, api := range own.rArgs {
// 		hashApis[hash] = api
// 	}
// 	return hashApis
// }

// 🔧 发送消息到特定hash的客户端
// func (own *RouterInfo) sendToHashClients(hash uint64, message, ndata interface{}) {
// 	// 快速收集客户端
// 	var clients []clientToNotify

// 	func() {
// 		own.websocketlock.RLock()
// 		defer own.websocketlock.RUnlock()

// 		if wsreq, ok := own.rWebSocketClient[hash]; ok {
// 			clients = make([]clientToNotify, 0, len(wsreq))
// 			for ws := range wsreq {
// 				if ws != nil && !ws.IsClosed() {
// 					hashStr := strconv.FormatUint(hash, 10)
// 					clients = append(clients, clientToNotify{
// 						ws:   ws,
// 						hash: hashStr,
// 						data: ndata,
// 					})
// 				}
// 			}
// 		}
// 	}()

// 	// 异步发送
// 	if len(clients) > 0 {
// 		go own.sendToClients(clients)
// 	}
// }

// type clientToNotify struct {
// 	ws   IWebSocket
// 	hash string
// 	data interface{}
// }

// // 🔧 修改 sendToClients，添加消息统计
// func (own *RouterInfo) sendToClients(clientsToNotify []clientToNotify) {
// 	const batchSize = 100

// 	// 🆕 记录广播
// 	own.recordWebSocketBroadcast(len(clientsToNotify))

// 	for i := 0; i < len(clientsToNotify); i += batchSize {
// 		end := i + batchSize
// 		if end > len(clientsToNotify) {
// 			end = len(clientsToNotify)
// 		}

// 		batch := clientsToNotify[i:end]
// 		go func(clients []clientToNotify) {
// 			for _, client := range clients {
// 				func() {
// 					defer func() {
// 						if err := recover(); err != nil {
// 							logx.Error("WebSocket发送失败:", err)
// 							// 🆕 记录错误
// 							own.recordWebSocketError()
// 						}
// 					}()

// 					done := make(chan bool, 1)
// 					go func() {
// 						defer func() {
// 							if err := recover(); err != nil {
// 								logx.Error("WebSocket Send panic:", err)
// 								own.recordWebSocketError()
// 							}
// 							done <- true
// 						}()

// 						// 🆕 计算消息大小
// 						var messageSize int
// 						if data, err := json.Marshal(client.data); err == nil {
// 							messageSize = len(data)
// 						}

// 						client.ws.Send(client.hash, own.Path, client.data)

// 						// 🆕 记录成功发送的消息
// 						own.recordWebSocketMessage(messageSize)
// 					}()

// 					select {
// 					case <-done:
// 						// 发送成功
// 					case <-time.After(5 * time.Second):
// 						logx.Errorf("WebSocket发送超时")
// 						own.recordWebSocketError()
// 					}
// 				}()
// 			}
// 		}(batch)

// 		if i+batchSize < len(clientsToNotify) {
// 			time.Sleep(10 * time.Millisecond)
// 		}
// 	}
// }

// func (own *RouterInfo) NoticeWebSocketClient(router IRouter, message interface{}) {
// 	own.webSocketHandler = false //关闭websocket代理处理

//		go own.noticeClient(router, message)
//	}
// func (own *RouterInfo) noticeClient(router IRouter, message interface{}) {
// 	// 先收集需要发送的客户端
// 	var clientsToNotify []struct {
// 		ws   IWebSocket
// 		data interface{}
// 	}

// 	own.websocketlock.Lock()
// 	hash := getApiHash(router)
// 	if wsreq, ok := own.rWebSocketClient[hash]; ok {
// 		for ws := range wsreq {
// 			if !ws.IsClosed() {
// 				var data interface{}
// 				if res, ok := message.(IResponse); ok {
// 					data = res.GetData()
// 				} else {
// 					data = message
// 				}
// 				clientsToNotify = append(clientsToNotify, struct {
// 					ws   IWebSocket
// 					data interface{}
// 				}{ws, data})
// 			}
// 		}
// 	}
// 	own.websocketlock.Unlock() // 只在这里解锁一次

// 	// 在锁外发送消息
// 	hashStr := strconv.FormatUint(hash, 10)
// 	for _, client := range clientsToNotify {
// 		client.ws.Send(hashStr, own.Path, client.data)
// 	}
// }

// 🔧 修改 CleanupDeadConnections，添加统计
// func (own *RouterInfo) CleanupDeadConnections() {
// 	own.websocketlock.Lock()
// 	defer own.websocketlock.Unlock()

// 	if own.rWebSocketClient == nil {
// 		return
// 	}

// 	var hashesToClean []uint64
// 	deadCount := 0

// 	for hash, clients := range own.rWebSocketClient {
// 		var deadClients []IWebSocket

// 		for ws := range clients {
// 			if ws == nil || ws.IsClosed() {
// 				deadClients = append(deadClients, ws)
// 			}
// 		}

// 		deadCount += len(deadClients)

// 		for _, ws := range deadClients {
// 			delete(clients, ws)
// 		}

// 		if len(clients) == 0 {
// 			hashesToClean = append(hashesToClean, hash)
// 		}
// 	}

// 	for _, hash := range hashesToClean {
// 		delete(own.rWebSocketClient, hash)
// 		delete(own.rArgs, hash)
// 	}

// 	if len(own.rArgs) == 0 {
// 		own.webSocketHandler = false
// 	}

// 	// 🆕 记录清理的死连接数
// 	if deadCount > 0 {
// 		own.recordDeadConnectionsCleaned(deadCount)
// 		logx.Infof("清理了 %d 个死连接，%d 个空hash", deadCount, len(hashesToClean))
// 	}
// }

// 🔧 新增：RouterInfo销毁时的清理
func (own *RouterInfo) Destroy() {
	// 🆕 先关闭统计系统
	own.closeStats()

	// 清理WebSocket连接
	own.CleanupDeadConnections()

	// 从全局清理map中移除
	key := own.Path
	if keyhash, ok := own.instance.(IRouterHashKey); ok {
		hashStr := strconv.FormatUint(keyhash.GetHashKey(), 10)
		key = key + ":" + hashStr
	}
	clearMap.Delete(key)

	logx.Infof("RouterInfo已销毁: %s", key)
}

var websocketcleanupOnce sync.Once
var clearMap sync.Map

var periodicWebSocketCleanup struct {
	sync.Mutex
	started bool
	stopped bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// func (own *RouterInfo) ensureWebSocketInit() {
// 	// 🔧 确保全局清理任务启动
// 	websocketcleanupOnce.Do(func() {
// 		logx.Info("🚀 启动全局WebSocket清理任务")
// 		StartPeriodicCleanup()
// 	})

// 	// 🔧 生成唯一的key
// 	key := own.ServiceName + ":" + own.Path
// 	if keyhash, ok := own.instance.(IRouterHashKey); ok {
// 		hashStr := strconv.FormatUint(keyhash.GetHashKey(), 10)
// 		key = key + ":" + hashStr
// 	}

// 	// 🔧 注册到全局清理map
// 	if _, loaded := clearMap.LoadOrStore(key, own); !loaded {
// 		logx.Infof("📝 注册WebSocket路由: %s", key)
// 	}
// }

func StartPeriodicCleanup() {
	registerWebSocketProcessShutdown()
	periodicWebSocketCleanup.Lock()
	if periodicWebSocketCleanup.started || periodicWebSocketCleanup.stopped {
		periodicWebSocketCleanup.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	periodicWebSocketCleanup.started = true
	periodicWebSocketCleanup.cancel = cancel
	periodicWebSocketCleanup.done = done
	periodicWebSocketCleanup.Unlock()

	go func() {
		defer close(done)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				totalCleaned := 0
				totalRouters := 0
				totalClients := 0

				clearMap.Range(func(key, value interface{}) bool {
					if rou, ok := value.(*RouterInfo); ok {
						totalRouters++
						beforeCount := rou.GetActiveClientCount()
						rou.CleanupDeadConnections()
						afterCount := rou.GetActiveClientCount()
						totalCleaned += beforeCount - afterCount
						totalClients += afterCount
					}
					return true
				})

				if totalCleaned > 0 || totalRouters > 0 {
					logx.Infof("WebSocket清理完成 - 路由数: %d, 活跃客户端: %d, 清理连接: %d",
						totalRouters, totalClients, totalCleaned)
				}
			}
		}
	}()
}

// StopPeriodicCleanup 停止进程级 WebSocket 周期清理；关闭后不允许在同一进程重启。
func StopPeriodicCleanup(ctx context.Context) error {
	periodicWebSocketCleanup.Lock()
	if !periodicWebSocketCleanup.stopped {
		periodicWebSocketCleanup.stopped = true
		if periodicWebSocketCleanup.cancel != nil {
			periodicWebSocketCleanup.cancel()
		}
	}
	done := periodicWebSocketCleanup.done
	periodicWebSocketCleanup.Unlock()
	if done == nil {
		return nil
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// GetActiveClientCount 返回当前活跃的websocket客户端数量
// func (own *RouterInfo) GetActiveClientCount() int {
// 	own.RLock()
// 	defer own.RUnlock()

// 	count := 0
// 	for _, clients := range own.rWebSocketClient {
// 		for ws := range clients {
// 			if !ws.IsClosed() {
// 				count++
// 			}
// 		}
// 	}
// 	return count
// }
