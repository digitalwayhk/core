package types

import (
	"encoding/json"
	"errors"
	"fmt"
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
	SpeedLimit        time.Duration //接口限速
	LimitType         int           //限速类型 0：IP,1:USERID
	IsTwoSteps        bool          //是否是二步验证访问
	PathType          ApiType
	StructName        string
	InstanceName      string
	instance          IRouter
	WebSocketWaitTime time.Duration                            //websocket默认通知的循环等待时间 默认:10秒
	iplasttime        map[string]time.Time                     //ip最后访问时间
	userlasttime      map[uint]time.Time                       //userid最后访问时间
	Subscriber        map[ObserveState]map[string]*ObserveArgs //订阅者
	rCache            sync.Map                                 //路由结果缓存,key:api hash,value:result
	rArgs             map[int]IRouter                          //路由参数
	rWebSocketClient  map[int]map[IWebSocket]IRequest          //websocket客户端
	webSocketHandler  bool                                     //websocket代理处理是否运行
	sync.RWMutex
}

func (own *RouterInfo) New() IRouter {
	item := utils.NewInterface(own.instance)
	if factory, ok := item.(IRouterFactory); ok {
		return factory.New(own.instance)
	}
	return item.(IRouter)
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
func (own *RouterInfo) limit(ip string, userid uint) error {
	if config.INITSERVER {
		return nil
	}
	own.Lock()
	defer own.Unlock()
	if own.iplasttime == nil {
		own.iplasttime = make(map[string]time.Time)
	}
	if lasttiem, ok := own.iplasttime[ip]; ok {
		if time.Now().Sub(lasttiem) < own.SpeedLimit {
			return errors.New("ip too many request")
		}
	} else {
		own.iplasttime[ip] = time.Now()
	}
	if own.LimitType == 1 {
		if own.userlasttime == nil {
			own.userlasttime = make(map[uint]time.Time)
		}
		if lasttiem, ok := own.userlasttime[userid]; ok {
			if time.Now().Sub(lasttiem) < own.SpeedLimit {
				return errors.New("user too many request")
			}
		} else {
			own.userlasttime[userid] = time.Now()
		}
	}
	return nil
}
func (own *RouterInfo) Exec(req IRequest) IResponse {
	uid, _ := req.GetUser()
	err := own.limit(req.GetClientIP(), uid)
	if err != nil {
		return req.NewResponse(nil, err)
	}
	api := own.New()
	err = api.Parse(req)
	if err != nil {
		msg := fmt.Sprintf("参数解析异常:%s", err)
		err = NewTypeError(own.ServiceName, own.Path, "parse", msg, 600)
		return req.NewResponse(nil, err)
	}
	return own.ExecDo(api, req)
}

func (own *RouterInfo) ExecDo(api IRouter, req IRequest) IResponse {
	err := api.Validation(req)
	if err != nil {
		msg := fmt.Sprintf("业务验证异常:%s", err)
		err = NewTypeError(own.ServiceName, own.Path, "validation", msg, 700)
		return req.NewResponse(nil, err)
	}
	go own.requestNotify(api, req.GetTraceId())
	data, err := api.Do(req)
	if err != nil {
		msg := fmt.Sprintf("调用执行异常:%s", err)
		err = NewTypeError(own.ServiceName, own.Path, "do", msg, 800)
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
	if _, ok := own.Subscriber[ob.State][ob.OwnAddress]; ok {
		delete(own.Subscriber[ob.State], ob.OwnAddress)
	}
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
			logx.Error(err)
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
			logx.Error(err)
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
			logx.Error(err)
		}
	}
}
func (own *RouterInfo) getCache(api IRouter) interface{} {
	key := getApiHash(api)
	if value, ok := own.rCache.Load(key); ok {
		return value
	}
	return nil
}
func (own *RouterInfo) setCache(api IRouter, value interface{}) {
	key := getApiHash(api)
	own.rCache.Store(key, value)
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

// 注册websocket的订阅，并返回订阅的event号
func (own *RouterInfo) RegisterWebSocketClient(router IRouter, client IWebSocket, req IRequest) string {
	if router == nil || client == nil || req == nil {
		return ""
	}
	own.Lock()
	defer own.Unlock()
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
	if _, ok := own.rWebSocketClient[hash]; !ok {
		own.rWebSocketClient[hash] = make(map[IWebSocket]IRequest, 0)
	}
	own.rWebSocketClient[hash][client] = req
	// if !own.webSocketHandler {
	// 	go own.webSocketHandlerRun()
	// }
	client.Send("sub", own.Path, strconv.Itoa(hash))
	return strconv.Itoa(hash)
}
func (own *RouterInfo) UnRegisterWebSocketClient(router IRouter, client IWebSocket) {
	if router == nil || client == nil {
		return
	}
	own.Lock()
	defer own.Unlock()
	hash := getApiHash(router)
	if _, ok := own.rWebSocketClient[hash]; ok {
		delete(own.rWebSocketClient[hash], client)
	}
	if len(own.rWebSocketClient[hash]) == 0 {
		delete(own.rWebSocketClient, hash)
		delete(own.rArgs, hash)
	}
	if len(own.rArgs) == 0 {
		own.webSocketHandler = false
	}
	client.Send("unsub", own.Path, strconv.Itoa(hash))
}
func (own *RouterInfo) webSocketHandlerRun() {
	if own.PathType != PublicType || own.webSocketHandler {
		return
	}
	own.webSocketHandler = true
	for {
		if !own.webSocketHandler {
			return
		}
		time.Sleep(own.WebSocketWaitTime)
		for hash, api := range own.rArgs {
			if wsreq, ok := own.rWebSocketClient[hash]; ok {
				var res IResponse = nil
				for ws, req := range wsreq {
					if res == nil {
						if cr, ok := req.(IRequestClear); ok {
							cr.ClearTraceId()
							cr.SetPath(own.Path)
						}
						res = own.ExecDo(api, req)
					}
					if !ws.IsClosed() {
						ws.Send(strconv.Itoa(hash), own.Path, res.GetData())
					}
				}
			}
		}
	}
}
func (own *RouterInfo) NoticeWebSocketClient(router IRouter, message interface{}) {
	own.webSocketHandler = false //关闭websocket代理处理
	go own.noticeClient(router, message)
}
func (own *RouterInfo) noticeClient(router IRouter, message interface{}) {
	defer own.Unlock()
	own.Lock()
	hash := getApiHash(router)
	if wsreq, ok := own.rWebSocketClient[hash]; ok {
		for ws := range wsreq {
			if !ws.IsClosed() {
				if res, ok := message.(IResponse); ok {
					ws.Send(strconv.Itoa(hash), own.Path, res.GetData())
				} else {
					ws.Send(strconv.Itoa(hash), own.Path, message)
				}
			}
		}
	}
}
