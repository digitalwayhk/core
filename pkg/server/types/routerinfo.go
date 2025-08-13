package types

import (
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/utils"

	"github.com/zeromicro/go-zero/core/logx"
)

type ApiType string

var (
	PublicType        ApiType = "public"
	PrivateType       ApiType = "private"
	ManageType        ApiType = "manage"
	ServerManagerType ApiType = "servermanager"
)

type RouterInfo struct {
	ID                int
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
	rArgs             map[int]IRouter                          //路由参数
	rWebSocketClient  map[int]map[IWebSocket]IRequest          //websocket客户端
	webSocketHandler  bool                                     //websocket代理处理是否运行
	sync.RWMutex
	pool          sync.Pool
	once          sync.Once
	TempStore     sync.Map
	websocketlock sync.RWMutex
}

func (own *RouterInfo) getNew() IRouter {
	defer func() {
		if err := recover(); err != nil {
			logx.Error(fmt.Sprintf("服务%s的路由%s发生异常:", own.ServiceName, own.Path), err)
		}
	}()
	return utils.NewInterface(own.instance).(IRouter)
	//有问题，值无法初始化
	// own.once.Do(func() {
	// 	own.pool = sync.Pool{
	// 		New: func() interface{} {
	// 			return utils.NewInterface(own.instance)
	// 		},
	// 	}
	// })
	// return own.pool.Get().(IRouter)
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
	//uid, _ := req.GetUser()
	// err := own.limit(req.GetClientIP(), uid)
	// if err != nil {
	// 	return req.NewResponse(nil, err)
	// }
	api := own.New()
	defer func() {
		if config.INITSERVER {
			return
		}
		//own.pool.Put(utils.NewInterface(api))
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

func (own *RouterInfo) ExecDo(api IRouter, req IRequest) IResponse {
	defer func() {
		//own.pool.Put(api)
		if config.INITSERVER {
			return
		}
		if err := recover(); err != nil {
			logx.Error(fmt.Sprintf("服务%s的路由%s发生异常:", own.ServiceName, own.Path), err)
			// 获取调用栈字符串并打印
			stack := debug.Stack()
			fmt.Printf("\nStack trace:\n%s\n", stack)
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
			resp := req.NewResponse(cache.data, nil)
			go own.responseNotify(api, req.GetTraceId(), resp)
			return resp
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
func getApiHash(api IRouter) int {
	key := ""
	utils.ForEach(api, func(name string, value interface{}) {
		key += utils.ConvertToString(value)
	})
	return utils.HashCode(key)
}
func (own *RouterInfo) GetWebSocketIRouter() []IRouter {
	items := make([]IRouter, 0)
	for _, r := range own.rArgs {
		items = append(items, r)
	}
	return items
}

// 注册websocket的订阅，并返回订阅的event号
func (own *RouterInfo) RegisterWebSocketClient(router IRouter, client IWebSocket, req IRequest) int {
	if router == nil || client == nil || req == nil {
		return 0
	}
	own.websocketlock.Lock()
	defer own.websocketlock.Unlock()
	if own.rArgs == nil {
		own.rArgs = make(map[int]IRouter, 0)
	}
	if own.rWebSocketClient == nil {
		own.rWebSocketClient = make(map[int]map[IWebSocket]IRequest, 0)
	}
	if own.PathType == PrivateType {
		id, _ := req.GetUser()
		utils.SetPropertyValue(router, "userid", id)
	}
	hash := getApiHash(router)
	if _, ok := own.rArgs[hash]; !ok {
		own.rArgs[hash] = router
	}
	if _, ok := own.rWebSocketClient[hash]; !ok || hash == 0 {
		own.rWebSocketClient[hash] = make(map[IWebSocket]IRequest, 0)
		if iwsr, ok := router.(IWebSocketRouter); ok {
			iwsr.RegisterWebSocket(client, req)
		}
	}
	own.rWebSocketClient[hash][client] = req
	//client.Send("sub", own.Path, strconv.Itoa(hash))

	return hash
}
func (own *RouterInfo) UnRegisterWebSocketClient(router IRouter, client IWebSocket) int {
	if router == nil || client == nil {
		return 0
	}
	hash := getApiHash(router)
	own.UnRegisterWebSocketHash(hash, client)
	return hash
}
func (own *RouterInfo) UnRegisterWebSocketHash(hash int, client IWebSocket) {
	if client == nil {
		return
	}
	own.websocketlock.Lock()
	defer own.websocketlock.Unlock()
	req := own.rWebSocketClient[hash][client]
	if _, ok := own.rWebSocketClient[hash]; ok {
		delete(own.rWebSocketClient[hash], client)
	}
	if len(own.rWebSocketClient[hash]) == 0 {
		api := own.rArgs[hash]
		if iwsr, ok := api.(IWebSocketRouter); ok {
			iwsr.UnRegisterWebSocket(client, req)
		}
		delete(own.rWebSocketClient, hash)
		delete(own.rArgs, hash)
	}
	if len(own.rArgs) == 0 {
		own.webSocketHandler = false
	}
}

// NoticeWebSocket 通知所有订阅的websocket客户端
// 这里假设 IWebSocketRouter 有一个 FiltersRouter 方法来过滤消息
// 该方法会遍历所有注册的路由，检查是否满足条件，并发送在NoticeFiltersRouter接口中返回的数据
// 注意：此方法会在锁内收集需要发送的客户端，避免在锁内直接发送消息，这样可以减少锁的持有时间，避免阻塞其他操作
func (own *RouterInfo) NoticeWebSocket(message interface{}) {
	if iwsr, ok := own.instance.(IWebSocketRouterNotice); ok {
		// 先收集需要发送的客户端
		var clientsToNotify []struct {
			ws   IWebSocket
			hash string
			data interface{}
		}

		own.websocketlock.RLock()
		for hash, api := range own.rArgs {
			if ok, ndata := iwsr.NoticeFiltersRouter(message, api); ok {
				if wsreq, ok := own.rWebSocketClient[hash]; ok {
					for ws := range wsreq {
						if !ws.IsClosed() {
							var data interface{}
							if res, ok := message.(IResponse); ok {
								data = res.GetData()
							} else {
								data = ndata
							}
							clientsToNotify = append(clientsToNotify, struct {
								ws   IWebSocket
								hash string
								data interface{}
							}{ws, strconv.Itoa(hash), data})
						}
					}
				}
			}
		}
		own.websocketlock.RUnlock()

		// 异步发送消息，避免阻塞
		go func() {
			for _, client := range clientsToNotify {
				// 添加错误恢复
				func() {
					defer func() {
						if err := recover(); err != nil {
							logx.Error(fmt.Sprintf("WebSocket发送失败: %v", err))
						}
					}()
					client.ws.Send(client.hash, own.Path, client.data)
				}()
			}
		}()
	}
}
func (own *RouterInfo) NoticeWebSocketClient(router IRouter, message interface{}) {
	own.webSocketHandler = false //关闭websocket代理处理

	go own.noticeClient(router, message)
}
func (own *RouterInfo) noticeClient(router IRouter, message interface{}) {
	// 先收集需要发送的客户端
	var clientsToNotify []struct {
		ws   IWebSocket
		data interface{}
	}

	own.websocketlock.Lock()
	hash := getApiHash(router)
	if wsreq, ok := own.rWebSocketClient[hash]; ok {
		for ws := range wsreq {
			if !ws.IsClosed() {
				var data interface{}
				if res, ok := message.(IResponse); ok {
					data = res.GetData()
				} else {
					data = message
				}
				clientsToNotify = append(clientsToNotify, struct {
					ws   IWebSocket
					data interface{}
				}{ws, data})
			}
		}
	}
	own.websocketlock.Unlock() // 只在这里解锁一次

	// 在锁外发送消息
	hashStr := strconv.Itoa(hash)
	for _, client := range clientsToNotify {
		client.ws.Send(hashStr, own.Path, client.data)
	}
}

// GetActiveClientCount 返回当前活跃的websocket客户端数量
func (own *RouterInfo) GetActiveClientCount() int {
	own.RLock()
	defer own.RUnlock()

	count := 0
	for _, clients := range own.rWebSocketClient {
		for ws := range clients {
			if !ws.IsClosed() {
				count++
			}
		}
	}
	return count
}
