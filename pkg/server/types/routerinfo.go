package types

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/utils"

	"github.com/zeromicro/go-zero/core/logx"
)

// IRouterResettable 可重置接口（对象池复用前重置状态）
type IRouterResettable interface {
	Reset()
}

// IRouterCleanable 可清理接口（对象池回收前清理敏感数据）
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
	//rWebSocketClient  map[uint64]map[IWebSocket]IRequest       //websocket客户端
	// 🔧 使用分片替代单一 map
	rWebSocketShards [shardCount]*websocketShard // 替代 rWebSocketClient
	//webSocketHandler  bool                                     //websocket代理处理是否运行
	sync.RWMutex
	pool          sync.Pool
	once          sync.Once
	TempStore     sync.Map
	websocketlock sync.RWMutex
	// 自定义响应处理函数
	ResponseHandlerFunc func(w http.ResponseWriter, r *http.Request, res IResponse) `json:"-"`
	channelPool         *ChannelPool

	// 🆕 性能统计字段
	stats     *RouterStats `json:"-"`
	statsLock sync.RWMutex
}

func (own *RouterInfo) New() IRouter {
	item := own.getNew()
	if factory, ok := item.(IRouterFactory); ok {
		return factory.New(own.instance)
	}
	return item
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
	own.instance = instance
}
func (own *RouterInfo) GetPath() string {
	return own.Path
}
func (own *RouterInfo) GetServiceName() string {
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
func (own *RouterInfo) Exec(req IRequest) IResponse {
	api := own.New()
	// 🔧 使用 defer 确保对象回收
	defer func() {
		if config.INITSERVER {
			return
		}

		// 🔧 回收对象到池
		own.putRouter(api)

		if err := recover(); err != nil {
			logx.Error(fmt.Sprintf("服务%s的路由%s发生异常:", own.ServiceName, own.Path), err)
		}
	}()
	err := api.Parse(req)
	if err != nil {
		msg := fmt.Sprintf("参数解析异常:%s", err)
		err = NewTypeError(own.ServiceName, own.Path, "parse", msg, 600)
		return req.NewResponse(nil, err)
	}
	return own.ExecDo(api, req)
}

// 🔧 修改 ExecDo 方法，添加统计
func (own *RouterInfo) ExecDo(api IRouter, req IRequest) IResponse {
	// 🆕 记录请求开始
	recordEnd := own.recordRequestStart()
	startTime := time.Now()

	defer func() {
		if config.INITSERVER {
			return
		}
		if err := recover(); err != nil {
			logx.Error(fmt.Sprintf("服务%s的路由%s发生异常:", own.ServiceName, own.Path), err)
			// 获取调用栈字符串并打印
			stack := debug.Stack()
			fmt.Printf("\nStack trace:\n%s\n", stack)

			// 🆕 记录异常
			own.recordRequestEnd(startTime, fmt.Errorf("%v", err))
		} else {
			// 🆕 正常结束
			recordEnd()
		}
	}()

	err := api.Validation(req)
	if err != nil {
		msg := fmt.Sprintf("业务验证异常:%s", err)
		err = NewTypeError(own.ServiceName, own.Path, "validation", msg, 700)
		logx.Error(err)
		return req.NewResponse(nil, err)
	}

	if own.useCache {
		if cache := own.getCache(api); cache != nil {
			// 🆕 记录缓存命中
			own.recordCacheHit()

			resp := req.NewResponse(cache.data, nil)
			go own.responseNotify(api, req.GetTraceId(), resp)
			return resp
		} else {
			// 🆕 记录缓存未命中
			own.recordCacheMiss()
		}
	}

	go own.requestNotify(api, req.GetTraceId())
	data, err := api.Do(req)
	if err != nil {
		msg := fmt.Sprintf("调用执行异常:%s", err)
		err = NewTypeError(own.ServiceName, own.Path, "do", msg, 800)
		logx.Error(err)
	} else {
		if own.useCache && data != nil {
			own.setCache(api, data)
		}
	}

	resp := req.NewResponse(data, err)
	if err != nil {
		go own.errorNotify(api, req.GetTraceId(), resp)
	} else {
		go own.responseNotify(api, req.GetTraceId(), resp)
	}
	return resp
}

func (own *RouterInfo) Subscribe(ob *ObserveArgs) error {
	own.Lock()
	defer own.Unlock()
	if own.Subscriber[ob.State] == nil {
		own.Subscriber[ob.State] = make(map[string]*ObserveArgs, 0)
	}
	if _, ok := own.Subscriber[ob.State][ob.OwnAddress]; ok {
		return nil //errors.New("subscriber already exists")
	}
	own.Subscriber[ob.State][ob.OwnAddress] = ob
	return nil
}
func (own *RouterInfo) UnSubscribe(ob *ObserveArgs) error {
	own.Lock()
	defer own.Unlock()
	if own.Subscriber[ob.State] == nil {
		return errors.New("subscriber not exists")
	}
	delete(own.Subscriber[ob.State], ob.OwnAddress)
	return nil
}
func (own *RouterInfo) requestNotify(api IRouter, traceid string) {
	items := own.Subscriber[ObserveRequest]
	for _, item := range items {
		item.ServiceName = own.ServiceName
		na := item.NewNotifyArgs(api, nil)
		na.TraceID = traceid
		err := item.Notify(na)
		if err != nil {
			logx.Error(err, item)
		}
	}
}
func (own *RouterInfo) responseNotify(api IRouter, traceid string, resp IResponse) {
	items := own.Subscriber[ObserveResponse]
	for _, item := range items {
		item.ServiceName = own.ServiceName
		na := item.NewNotifyArgs(api, resp)
		na.TraceID = traceid
		err := item.Notify(na)
		if err != nil {
			logx.Error(err, item)
		}
	}
}
func (own *RouterInfo) errorNotify(api IRouter, traceid string, resp IResponse) {
	items := own.Subscriber[ObserveError]
	for _, item := range items {
		item.ServiceName = own.ServiceName
		na := item.NewNotifyArgs(api, resp)
		na.TraceID = traceid
		err := item.Notify(na)
		if err != nil {
			logx.Error(err, item)
		}
	}
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
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			totalCleaned := 0
			totalRouters := 0
			totalClients := 0

			clearMap.Range(func(key, value interface{}) bool {
				if rou, ok := value.(*RouterInfo); ok {
					totalRouters++

					// 🔧 统计清理前的客户端数量
					beforeCount := rou.GetActiveClientCount()
					rou.CleanupDeadConnections()
					afterCount := rou.GetActiveClientCount()

					cleaned := beforeCount - afterCount
					totalCleaned += cleaned
					totalClients += afterCount
				}
				return true
			})

			if totalCleaned > 0 || totalRouters > 0 {
				logx.Infof("🧹 WebSocket清理完成 - 路由数: %d, 活跃客户端: %d, 清理连接: %d",
					totalRouters, totalClients, totalCleaned)
			}
		}
	}()
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
