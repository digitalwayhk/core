package melody

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/olahol/melody"
	"github.com/zeromicro/go-zero/core/logx"
)

type MessageEvent string

const (
	//订阅
	Subscribe MessageEvent = "sub"
	//取消订阅
	UnSubscribe MessageEvent = "unsub"
	//调用，调用后返回结果
	Call MessageEvent = "call"
	//获取订阅信息
	Get MessageEvent = "get"
)

type Message struct {
	Event   string      `json:"event"`
	Channel string      `json:"channel"`
	Data    interface{} `json:"data"`
}

// MelodyManager 替换原有的Hub
type MelodyManager struct {
	melody         *melody.Melody
	serviceContext *router.ServiceContext

	// 添加关闭通道
	closeChan chan struct{}
	closed    bool
	closeMu   sync.Mutex

	// 客户端订阅管理
	subscriptions   map[*melody.Session]*SessionSubscriptions
	subscriptionsMu sync.RWMutex

	// 统计信息
	stats struct {
		totalConnections   int64
		activeConnections  int64
		totalMessages      int64
		totalSubscriptions int64
		mu                 sync.RWMutex
	}
}

func NewMelodyManager(serviceContext *router.ServiceContext) *MelodyManager {
	m := melody.New()

	// 配置Melody参数
	m.Config.MaxMessageSize = 512
	m.Config.MessageBufferSize = 256
	m.Config.PongWait = 60 * time.Second
	m.Config.PingPeriod = 54 * time.Second
	m.Config.WriteWait = 10 * time.Second
	// m.Config.ReadBufferSize = 1024
	// m.Config.WriteBufferSize = 1024

	manager := &MelodyManager{
		melody:         m,
		serviceContext: serviceContext,
		subscriptions:  make(map[*melody.Session]*SessionSubscriptions),
		closeChan:      make(chan struct{}), // 🔧 添加关闭通道
	}

	manager.setupHandlers()
	return manager
}

// 🔧 修复：安全的统计监控
func (mm *MelodyManager) startStatsMonitor() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			mm.stats.mu.RLock()
			logx.Infof("WebSocket统计 - 总连接: %d, 活跃连接: %d, 总消息: %d, 总订阅: %d",
				mm.stats.totalConnections,
				mm.stats.activeConnections,
				mm.stats.totalMessages,
				mm.stats.totalSubscriptions)
			mm.stats.mu.RUnlock()
		case <-mm.closeChan: // 🔧 添加退出机制
			logx.Info("WebSocket统计监控退出")
			return
		}
	}
}

// 🔧 修复：优雅关闭
func (mm *MelodyManager) Close() error {
	mm.closeMu.Lock()
	defer mm.closeMu.Unlock()

	if mm.closed {
		return nil
	}

	mm.closed = true
	close(mm.closeChan) // 关闭统计监控goroutine

	return mm.melody.Close()
}

func (mm *MelodyManager) setupHandlers() {
	// 连接建立事件
	mm.melody.HandleConnect(func(s *melody.Session) {
		mm.onConnect(s)
	})

	// 连接断开事件
	mm.melody.HandleDisconnect(func(s *melody.Session) {
		mm.onDisconnect(s)
	})

	// 消息处理事件
	mm.melody.HandleMessage(func(s *melody.Session, data []byte) {
		mm.handleMessage(s, data)
	})

	// 错误处理事件
	mm.melody.HandleError(func(s *melody.Session, err error) {
		logx.Errorf("WebSocket错误: %v, RemoteAddr: %s", err, s.Request.RemoteAddr)
	})

	// 定期统计
	go mm.startStatsMonitor()
}

func (mm *MelodyManager) onConnect(s *melody.Session) {
	// 创建请求对象
	req := router.NewRequest(mm.serviceContext.Router, s.Request)
	s.Set("request", req)
	client := &MelodyClient{
		session: s,
		manager: mm,
	}
	s.Set("client", client)
	// 初始化订阅映射
	mm.subscriptionsMu.Lock()
	mm.subscriptions[s] = NewSessionSubscriptions(mm)
	mm.subscriptionsMu.Unlock()

	// 🔧 修复：在锁内读取和更新统计
	mm.stats.mu.Lock()
	mm.stats.totalConnections++
	mm.stats.activeConnections++
	activeCount := mm.stats.activeConnections // 在锁内读取
	mm.stats.mu.Unlock()

	logx.Infof("WebSocket客户端连接: %s, 当前活跃连接: %d",
		s.Request.RemoteAddr, activeCount)
}

func (mm *MelodyManager) onDisconnect(s *melody.Session) {
	mm.cleanupSession(s)
	s.UnSet("request") // 清理请求对象
	s.UnSet("client")  // 清理客户端适配器
	mm.subscriptionsMu.Lock()
	delete(mm.subscriptions, s) // 删除订阅映射
	mm.subscriptionsMu.Unlock()

	// 🔧 修复：同样在锁内操作
	mm.stats.mu.Lock()
	mm.stats.activeConnections--
	activeCount := mm.stats.activeConnections
	mm.stats.mu.Unlock()

	logx.Infof("WebSocket客户端断开: %s, 当前活跃连接: %d",
		s.Request.RemoteAddr, activeCount)
}
func (mm *MelodyManager) handleMessage(s *melody.Session, data []byte) {
	// 🔧 添加：恐慌恢复
	defer func() {
		if err := recover(); err != nil {
			logx.Errorf("WebSocket消息处理发生恐慌: %v, RemoteAddr: %s", err, s.Request.RemoteAddr)
			mm.sendError(s, "", "服务器内部错误")
		}
	}()

	mm.stats.mu.Lock()
	mm.stats.totalMessages++
	mm.stats.mu.Unlock()

	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		mm.sendError(s, "", "消息格式错误: "+err.Error())
		return
	}

	switch MessageEvent(msg.Event) {
	case Get:
		mm.handleGet(s, &msg)
	case Call:
		mm.handleCall(s, &msg)
	case Subscribe:
		mm.handleSubscribe(s, &msg)
	case UnSubscribe:
		mm.handleUnsubscribe(s, &msg)
	default:
		mm.sendError(s, msg.Channel, "不支持的事件类型: "+msg.Event)
	}
}
func (mm *MelodyManager) handleGet(s *melody.Session, msg *Message) {
	mm.subscriptionsMu.RLock()
	subscriptions := mm.subscriptions[s]
	mm.subscriptionsMu.RUnlock()

	if msg.Channel != "" {
		mm.sendToSession(s, msg.Event, msg.Channel, subscriptions.GetSubscriptions(msg.Channel))
	} else {
		mm.sendToSession(s, msg.Event, msg.Channel, subscriptions.GetAllSubscriptions())
	}
}

func (mm *MelodyManager) handleCall(s *melody.Session, msg *Message) {
	msg.Channel = strings.TrimSpace(msg.Channel)
	info := mm.serviceContext.Router.GetRouter(msg.Channel)
	if info == nil {
		mm.sendError(s, msg.Channel, "当前服务中未找到对应的路由")
		return
	}

	// 获取请求对象
	reqInterface, exists := s.Get("request")
	if !exists {
		mm.sendError(s, msg.Channel, "内部错误：无法获取请求对象")
		return
	}
	req := reqInterface.(types.IRequest)

	// 清理请求状态
	if cr, ok := req.(types.IRequestClear); ok {
		cr.ClearTraceId()
		cr.SetPath(msg.Channel)
	}

	// 解析请求数据
	api, err := mm.parseRequest(info, msg.Data)
	if err != nil {
		mm.sendError(s, msg.Channel, "数据格式不正确: "+err.Error())
		return
	}

	// 执行调用
	res := info.ExecDo(api, req)
	mm.sendToSession(s, msg.Event, msg.Channel, res)
}

func (mm *MelodyManager) handleSubscribe(s *melody.Session, msg *Message) {
	msg.Channel = strings.TrimSpace(msg.Channel)
	info := mm.serviceContext.Router.GetRouter(msg.Channel)
	if info == nil {
		mm.sendError(s, msg.Channel, "当前服务中未找到对应的路由")
		return
	}

	// 获取请求对象
	reqInterface, exists := s.Get("request")
	if !exists {
		mm.sendError(s, msg.Channel, "内部错误：无法获取请求对象")
		return
	}
	req := reqInterface.(types.IRequest)

	// 解析订阅数据
	api, err := mm.parseRequest(info, msg.Data)
	if err != nil {
		mm.sendError(s, msg.Channel, "订阅错误: "+err.Error())
		return
	}

	// 验证订阅请求
	err = api.Validation(req)
	if err != nil {
		mm.sendError(s, msg.Channel, "订阅错误: "+err.Error())
		return
	}

	// 创建Melody客户端适配器
	var melodyClient *MelodyClient
	if client, exists := s.Get("client"); exists {
		melodyClient = client.(*MelodyClient)
	} else {
		melodyClient = &MelodyClient{session: s, manager: mm}
	}
	// 注册订阅
	hash := info.RegisterWebSocketClient(api, melodyClient, req)
	//	melodyClient.SubChannel(msg.Channel, hash, api)
	// // 保存订阅信息
	// mm.subscriptionsMu.Lock()
	// if _, ok := mm.subscriptions[s][msg.Channel]; !ok {
	// 	mm.subscriptions[s][msg.Channel] = make(map[int]types.IRouter)
	// }
	// mm.subscriptions[s][msg.Channel][hash] = api
	// subscriptions := mm.subscriptions[s]
	// mm.subscriptionsMu.Unlock()

	// 更新统计
	mm.stats.mu.Lock()
	mm.stats.totalSubscriptions++
	mm.stats.mu.Unlock()

	mm.sendToSession(s, msg.Event, msg.Channel, melodyClient.GetChannelArgs(msg.Channel))
	logx.Infof("客户端订阅成功: %s, 频道: %s, Hash: %d", s.Request.RemoteAddr, msg.Channel, hash)
}

func (mm *MelodyManager) handleUnsubscribe(s *melody.Session, msg *Message) {
	var melodyClient *MelodyClient
	if client, exists := s.Get("client"); exists {
		melodyClient = client.(*MelodyClient)
	}
	if melodyClient == nil {
		mm.sendError(s, msg.Channel, "内部错误：无法获取客户端对象")
		return
	}
	msg.Channel = strings.TrimSpace(msg.Channel)
	info := mm.serviceContext.Router.GetRouter(msg.Channel)
	if info == nil {
		mm.sendError(s, msg.Channel, "当前服务中未找到对应的路由")
		return
	}
	// 普通退订

	var hash int
	if hs, ok := msg.Data.(int); ok && hs > 0 {
		hash = int(hs)
		info.UnRegisterWebSocketHash(hash, melodyClient)
	} else {
		reqInterface, exists := s.Get("request")
		if !exists {
			mm.sendError(s, msg.Channel, "内部错误：无法获取请求对象")
			return
		}
		req := reqInterface.(types.IRequest)
		api, err := mm.parseRequest(info, msg.Data)
		if err != nil {
			mm.sendError(s, msg.Channel, "退订错误: "+err.Error())
			return
		}
		err = api.Validation(req)
		if err != nil {
			mm.sendError(s, msg.Channel, "退订错误: "+err.Error())
			return
		}
		hash = info.UnRegisterWebSocketClient(api, melodyClient)
	}
	//melodyClient.UnsubChannel(msg.Channel, hash)
}

func (mm *MelodyManager) parseRequest(info *types.RouterInfo, data interface{}) (types.IRouter, error) {
	defer func() {
		if err := recover(); err != nil {
			logx.Errorf("服务%s的路由%s发生异常:ParseNew, error: %v", info.ServiceName, info.Path, err)
		}
	}()

	var api types.IRouter
	var err error
	if data == nil {
		api = info.New()
	} else {
		api, err = info.ParseNew(data)
	}
	return api, err
}

func (mm *MelodyManager) cleanupSession(s *melody.Session) {
	mm.subscriptionsMu.Lock()
	defer mm.subscriptionsMu.Unlock()

	// subscriptions, exists := mm.subscriptions[s]
	// if !exists {
	// 	return
	// }
	// 清理所有订阅
	//melodyClient := &MelodyClient{session: s, manager: mm}
	// for channel, items := range subscriptions {
	// 	info := mm.serviceContext.Router.GetRouter(channel)
	// 	if info != nil {
	// 		for hash := range items {
	// 			info.UnRegisterWebSocketHash(hash, melodyClient)
	// 		}
	// 	}
	// }

	delete(mm.subscriptions, s)
	//logx.Infof("清理客户端订阅: %s, 清理频道数: %d", s.Request.RemoteAddr, len(subscriptions))
}

func (mm *MelodyManager) sendToSession(s *melody.Session, event, channel string, data interface{}) {
	msg := &Message{
		Event:   event,
		Channel: channel,
		Data:    data,
	}
	// 处理JSON序列化错误
	msgData, err := json.Marshal(msg)
	if err != nil {
		logx.Errorf("JSON序列化失败: %v, event: %s, channel: %s", err, event, channel)
		// 发送简化的错误消息
		errorMsg := &Message{
			Event:   event,
			Channel: channel,
			Data:    "消息格式化错误: " + err.Error(),
		}
		if errorData, err := json.Marshal(errorMsg); err == nil {
			s.Write(errorData)
		}
		return
	}

	// 🔧 修复：处理WebSocket写入错误
	if err := s.Write(msgData); err != nil {
		logx.Errorf("WebSocket写入失败: %v, RemoteAddr: %s", err, s.Request.RemoteAddr)
		// 可以考虑标记连接为已断开或触发重连逻辑
	}
}

func (mm *MelodyManager) sendError(s *melody.Session, channel, errMsg string) {
	// 🔧 修复：使用更安全的错误发送
	errorMsg := &Message{
		Event:   "error",
		Channel: channel,
		Data:    errMsg,
	}

	msgData, err := json.Marshal(errorMsg)
	if err != nil {
		logx.Errorf("错误消息序列化失败: %v", err)
		return
	}

	if err := s.Write(msgData); err != nil {
		logx.Errorf("发送错误消息失败: %v, RemoteAddr: %s", err, s.Request.RemoteAddr)
	}
}

// WebSocket路由处理器
func (mm *MelodyManager) ServeWS(w http.ResponseWriter, r *http.Request) {
	mm.melody.HandleRequest(w, r)
}

// 获取统计信息
func (mm *MelodyManager) GetStats() map[string]interface{} {
	mm.stats.mu.RLock()
	defer mm.stats.mu.RUnlock()

	return map[string]interface{}{
		"total_connections":   mm.stats.totalConnections,
		"active_connections":  mm.stats.activeConnections,
		"total_messages":      mm.stats.totalMessages,
		"total_subscriptions": mm.stats.totalSubscriptions,
		"subscriptions_count": len(mm.subscriptions),
	}
}

// 广播消息到所有客户端
func (mm *MelodyManager) Broadcast(data []byte) {
	mm.melody.Broadcast(data)
}

// 广播消息到特定条件的客户端
func (mm *MelodyManager) BroadcastFilter(data []byte, filter func(*melody.Session) bool) {
	mm.melody.BroadcastFilter(data, filter)
}

func (mm *MelodyManager) GetSessionSubscriptions(s *melody.Session) map[string]map[int]types.IRouter {
	// mm.sessionMu.RLock()
	// defer mm.sessionMu.RUnlock()

	// if sessionSub, exists := mm.sessionSubscriptions[s]; exists {
	// 	sessionSub.mu.RLock()
	// 	defer sessionSub.mu.RUnlock()

	// 	// 返回副本，避免并发修改
	// 	result := make(map[string]map[int]types.IRouter)
	// 	for channel, hashMap := range sessionSub.subscriptions {
	// 		result[channel] = make(map[int]types.IRouter)
	// 		for hash, router := range hashMap {
	// 			result[channel][hash] = router
	// 		}
	// 	}
	// 	return result
	// }

	return make(map[string]map[int]types.IRouter)
}

// 🔧 获取特定频道的订阅
func (mm *MelodyManager) GetChannelSubscriptions(s *melody.Session, channel string) map[int]types.IRouter {
	// mm.sessionMu.RLock()
	// defer mm.sessionMu.RUnlock()

	// if sessionSub, exists := mm.sessionSubscriptions[s]; exists {
	// 	sessionSub.mu.RLock()
	// 	defer sessionSub.mu.RUnlock()

	// 	if channelSubs, exists := sessionSub.subscriptions[channel]; exists {
	// 		// 返回副本
	// 		result := make(map[int]types.IRouter)
	// 		for hash, router := range channelSubs {
	// 			result[hash] = router
	// 		}
	// 		return result
	// 	}
	// }

	return make(map[int]types.IRouter)
}

// 🔧 添加订阅
func (mm *MelodyManager) AddSessionSubscription(s *melody.Session, channel string, hash int, router types.IRouter) {
	// mm.sessionMu.Lock()
	// defer mm.sessionMu.Unlock()

	// // 确保SessionSubscriptions存在
	// if _, exists := mm.sessionSubscriptions[s]; !exists {
	// 	mm.sessionSubscriptions[s] = &SessionSubscriptions{
	// 		subscriptions: make(map[string]map[int]types.IRouter),
	// 		metadata:      make(map[string]interface{}),
	// 		createdAt:     time.Now(),
	// 		lastActivity:  time.Now(),
	// 	}
	// }

	// sessionSub := mm.sessionSubscriptions[s]
	// sessionSub.mu.Lock()
	// defer sessionSub.mu.Unlock()

	// // 确保频道存在
	// if _, exists := sessionSub.subscriptions[channel]; !exists {
	// 	sessionSub.subscriptions[channel] = make(map[int]types.IRouter)
	// }

	// sessionSub.subscriptions[channel][hash] = router
	// sessionSub.lastActivity = time.Now()

	logx.Infof("添加订阅: Session=%p, Channel=%s, Hash=%d", s, channel, hash)
}

// 🔧 移除订阅
func (mm *MelodyManager) RemoveSessionSubscription(s *melody.Session, channel string, hash int) bool {
	mm.subscriptionsMu.Lock()
	subscript := mm.subscriptions[s]
	mm.subscriptionsMu.Unlock()
	if subscript == nil {
		logx.Errorf("尝试移除订阅时，未找到对应的Session: %p", s)
		return false
	}
	subscript.GetSubscriptions(channel)

	subscript.mu.Lock()
	defer subscript.mu.Unlock()

	// channelSubs, exists := sessionSub.subscriptions[channel]
	// if !exists {
	// 	return false
	// }

	// if _, exists := channelSubs[hash]; !exists {
	// 	return false
	// }

	// delete(channelSubs, hash)
	// sessionSub.lastActivity = time.Now()

	// // 如果频道没有订阅了，删除频道
	// if len(channelSubs) == 0 {
	// 	delete(sessionSub.subscriptions, channel)
	// }

	logx.Infof("移除订阅: Session=%p, Channel=%s, Hash=%d", s, channel, hash)
	return true
}

// 🔧 检查是否有订阅
func (mm *MelodyManager) HasSubscription(s *melody.Session, channel string, hash int) bool {
	// mm.sessionMu.RLock()
	// defer mm.sessionMu.RUnlock()

	// if sessionSub, exists := mm.sessionSubscriptions[s]; exists {
	// 	sessionSub.mu.RLock()
	// 	defer sessionSub.mu.RUnlock()

	// 	if channelSubs, exists := sessionSub.subscriptions[channel]; exists {
	// 		_, exists := channelSubs[hash]
	// 		return exists
	// 	}
	// }

	return false
}
