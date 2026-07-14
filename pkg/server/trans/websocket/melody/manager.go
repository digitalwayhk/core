package melody

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
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
	closeChan   chan struct{}
	monitorDone chan struct{}
	closed      bool
	closeMu     sync.Mutex

	// 客户端订阅管理
	subscriptions   map[*melody.Session]*SessionSubscriptions
	subscriptionsMu sync.RWMutex
	connectionLimit *ConnectionRateLimiter

	connCounter    *ConnectionCounter //
	maxConnections int64              // 最大连接数限制

	// 统计信息
	stats struct {
		totalConnections   int64
		activeConnections  int64
		totalMessages      int64
		totalSubscriptions int64
		mu                 sync.RWMutex
	}
}

func NewMelodyManager(serviceContext *router.ServiceContext, options ...config.MelodyConfigOption) *MelodyManager {
	m := melody.New()
	config := config.GetMelodyConfig()
	for _, option := range options {
		option(config)
	}
	// 配置Melody参数
	m.Config.MaxMessageSize = config.MaxMessageSize
	m.Config.MessageBufferSize = config.MessageBufferSize
	m.Config.PongWait = config.PongWait
	m.Config.PingPeriod = config.PingPeriod
	m.Config.WriteWait = config.WriteWait

	// m.Config.WriteBufferSize = 1024

	manager := &MelodyManager{
		melody:          m,
		serviceContext:  serviceContext,
		subscriptions:   make(map[*melody.Session]*SessionSubscriptions),
		closeChan:       make(chan struct{}), // 🔧 添加关闭通道
		monitorDone:     make(chan struct{}),
		connectionLimit: NewConnectionRateLimiter(),
		connCounter:     &ConnectionCounter{},
		maxConnections:  config.MaxConnections, // 默认最大连接数
	}

	manager.setupHandlers()
	return manager
}
func (mm *MelodyManager) GetMelody() *melody.Melody {
	return mm.melody
}
func (mm *MelodyManager) GetConnectionCounter() *ConnectionCounter {
	return mm.connCounter
}
func (mm *MelodyManager) GetMaxConnections() int64 {
	return mm.maxConnections
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
func (mm *MelodyManager) GetConnectionLimiter() *ConnectionRateLimiter {
	return mm.connectionLimit
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
	mm.connectionLimit.Close()
	<-mm.monitorDone

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
		errMsg := err.Error()

		// 检查是否是正常的客户端断开
		if strings.Contains(errMsg, "close 1001") ||
			strings.Contains(errMsg, "going away") {
			// 客户端正常离开，使用Info级别
			logx.Infof("WebSocket客户端正常断开: %s", s.Request.RemoteAddr)
			return
		}

		// 检查是否是其他正常的关闭码
		if strings.Contains(errMsg, "close 1000") { // 正常关闭
			logx.Infof("WebSocket连接正常关闭: %s", s.Request.RemoteAddr)
			return
		}

		// 检查缓冲区满的问题
		if strings.Contains(errMsg, "message buffer is full") {
			logx.Errorf("WebSocket缓冲区满，强制断开连接: %s", s.Request.RemoteAddr)
			mm.handleBufferFullError(s)
			return
		}

		// 其他错误使用Error级别
		logx.Errorf("WebSocket异常错误: %v, RemoteAddr: %s", err, s.Request.RemoteAddr)
	})

	// 定期统计
	go func() {
		defer close(mm.monitorDone)
		mm.startStatsMonitor()
	}()
}

// 🔧 新增：处理缓冲区满的错误
func (mm *MelodyManager) handleBufferFullError(s *melody.Session) {
	// 防止重复处理
	if s.IsClosed() {
		return
	}

	// 标记为问题连接
	s.Set("buffer_full", true)

	// 先清理订阅状态
	//mm.cleanupSession(s)

	// 强制关闭连接
	go func() {
		// 给一点时间让清理完成
		time.Sleep(50 * time.Millisecond)

		// 真正关闭WebSocket连接
		err := s.Close()
		if err != nil {
			logx.Errorf("强制关闭WebSocket连接失败: %v, RemoteAddr: %s", err, s.Request.RemoteAddr)
		} else {
			logx.Infof("WebSocket连接已强制关闭: %s", s.Request.RemoteAddr)
		}
	}()
}
func (mm *MelodyManager) onConnect(s *melody.Session) {
	currentCount := mm.connCounter.Increment()
	if currentCount > mm.maxConnections {
		logx.Errorf("超过最大连接数限制 %d，拒绝连接: %s", mm.maxConnections, s.Request.RemoteAddr)
		mm.connCounter.Decrement()
		s.Close()
		return
	}

	// 🔧 在锁外创建所有对象
	client := &MelodyClient{
		session: s,
		manager: mm,
	}
	sr := mm.serviceContext.Router
	newSubscriptions := NewSessionSubscriptions(mm, client, sr)

	// 🔧 最小化锁持有时间 - 只保护map操作
	mm.subscriptionsMu.Lock()
	mm.subscriptions[s] = newSubscriptions
	mm.subscriptionsMu.Unlock()

	// 🔧 统计更新
	mm.stats.mu.Lock()
	mm.stats.totalConnections++
	mm.stats.activeConnections++
	activeCount := mm.stats.activeConnections
	mm.stats.mu.Unlock()

	logx.Infof("WebSocket客户端连接: %s, 当前活跃连接: %d",
		s.Request.RemoteAddr, activeCount)
}

func (mm *MelodyManager) onDisconnect(s *melody.Session) {
	currentCount := mm.connCounter.Decrement()
	mm.cleanupSession(s)

	// 同样在锁内操作
	mm.stats.mu.Lock()
	mm.stats.activeConnections--
	activeCount := mm.stats.activeConnections
	mm.stats.mu.Unlock()

	logx.Infof("WebSocket客户端断开: %s, 当前活跃连接: %d, 当前连接数: %d",
		s.Request.RemoteAddr, activeCount, currentCount)
}

func (mm *MelodyManager) handleMessage(s *melody.Session, data []byte) {
	// defer func() {
	// 	if err := recover(); err != nil {
	// 		logx.Errorf("WebSocket消息处理发生恐慌: %v, RemoteAddr: %s", err, s.Request.RemoteAddr)
	// 		mm.sendError(s, "", "服务器内部错误")
	// 	}
	// }()
	dataLen := len(data)
	preview := string(data)
	if dataLen > int(mm.melody.Config.MaxMessageSize) {
		preview = preview[:int(mm.melody.Config.MaxMessageSize)] + "...(truncated)"
	}
	mm.stats.mu.Lock()
	mm.stats.totalMessages++
	mm.stats.mu.Unlock()

	msg := &Message{}

	if err := json.Unmarshal(data, msg); err != nil {
		// 解析失败时记录更多信息
		logx.Errorf("JSON 解析失败: %v, len=%d, preview=%s, RemoteAddr=%s", err, dataLen, preview, s.Request.RemoteAddr)
		// 1) 先去掉尾部空白
		tryBuf := bytes.TrimSpace(data)

		// 2) 容错：移除尾随的逗号（例如 "...} , "）再尝试解析
		re := regexp.MustCompile(",\\s*([\\}\\]])")
		fixed := re.ReplaceAllString(string(tryBuf), "$1")

		if err2 := json.Unmarshal([]byte(fixed), msg); err2 == nil {
			logx.Errorf("JSON 解析通过移除尾随逗号成功, 原len=%d, newlen=%d, RemoteAddr=%s", dataLen, len(fixed), s.Request.RemoteAddr)
		} else {
			// 3) 继续尝试剔除末尾反斜线并重试（最多5次）
			parsed := false
			tmp := []byte(fixed)
			for i := 0; i < 5 && len(tmp) > 0; i++ {
				last := tmp[len(tmp)-1]
				if last == '\\' || last == ',' || last == '\n' || last == '\r' || last == '\t' || last == ' ' {
					tmp = tmp[:len(tmp)-1]
					if err3 := json.Unmarshal(tmp, msg); err3 == nil {
						parsed = true
						logx.Errorf("JSON 解析通过逐次修剪末尾字符成功, 原len=%d, newlen=%d, RemoteAddr=%s", dataLen, len(tmp), s.Request.RemoteAddr)
						break
					}
					continue
				}
				break
			}
			if !parsed {
				// 4) 最后一招：用 Decoder 读取第一个 JSON 对象（忽略尾部垃圾）
				dec := json.NewDecoder(bytes.NewReader(data))
				dec.UseNumber()
				if err4 := dec.Decode(&msg); err4 == nil {
					logx.Errorf("JSON 使用 Decoder 解析成功（忽略尾部）， RemoteAddr=%s", s.Request.RemoteAddr)
				} else {
					mm.sendError(s, "", "消息格式错误: "+err.Error())
					return
				}
			}
		}
	}

	switch MessageEvent(msg.Event) {
	case Get:
		mm.handleGet(s, msg)
	case Call:
		mm.handleCall(s, msg)
	case Subscribe:
		mm.handleSubscribe(s, msg)
	case UnSubscribe:
		mm.handleUnsubscribe(s, msg)
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
	mm.subscriptionsMu.RLock()
	subscriptions := mm.subscriptions[s]
	mm.subscriptionsMu.RUnlock()
	channel := strings.TrimSpace(msg.Channel)
	req := subscriptions.getIRequest(channel)
	info := mm.serviceContext.Router.GetRouter(channel)
	if info == nil {
		mm.sendError(s, channel, "当前服务中未找到对应的路由")
		return
	}
	// 解析请求数据
	api, err := mm.parseRequest(info, msg.Data)
	if err != nil {
		mm.sendError(s, channel, "数据格式不正确: "+err.Error())
		return
	}

	// 执行调用
	res := info.ExecDo(api, req)
	mm.sendToSession(s, msg.Event, msg.Channel, res)
}

func (mm *MelodyManager) handleSubscribe(s *melody.Session, msg *Message) {
	// 🔧 使用读锁并快速获取引用
	mm.subscriptionsMu.RLock()
	subscriptions := mm.subscriptions[s]
	mm.subscriptionsMu.RUnlock()

	if subscriptions == nil {
		mm.sendError(s, msg.Channel, "会话未初始化")
		return
	}

	// 🔧 在锁外设置router
	subscriptions.setServiceRouter(mm.serviceContext.Router)
	if subscriptions.HandleSubscribe(msg) {
		logx.Infof("客户端订阅成功: %s, 频道: %s", s.Request.RemoteAddr, msg.Channel)
	}
}

func (mm *MelodyManager) handleUnsubscribe(s *melody.Session, msg *Message) {
	mm.subscriptionsMu.RLock()
	subscriptions := mm.subscriptions[s]
	mm.subscriptionsMu.RUnlock()

	if subscriptions == nil {
		mm.sendError(s, msg.Channel, "会话未初始化")
		return
	}

	subscriptions.setServiceRouter(mm.serviceContext.Router)
	subscriptions.HandleUnsubscribe(msg)

	logx.Infof("客户端退订成功: %s, 频道: %s", s.Request.RemoteAddr, msg.Channel)
}

func (mm *MelodyManager) parseRequest(info *types.RouterInfo, data interface{}) (api types.IRouter, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logx.Errorf("服务%s的路由%s发生异常:ParseNew, error: %v", info.ServiceName, info.Path, recovered)
			err = fmt.Errorf("parse websocket call request: %v", recovered)
			api = nil
		}
	}()

	if data == nil {
		api = info.New()
	} else {
		api, err = info.ParseNew(data)
	}
	return api, err
}

func (mm *MelodyManager) parseSubscriptionRequest(info *types.RouterInfo, data interface{}) (api types.IRouter, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logx.Errorf("服务%s的路由%s发生异常:ParseSubscription, error: %v", info.ServiceName, info.Path, recovered)
			err = fmt.Errorf("parse websocket subscription request: %v", recovered)
			api = nil
		}
	}()
	if data == nil {
		api = info.NewSubscription()
		if api == nil {
			return nil, errors.New("创建 WebSocket 订阅实例失败")
		}
		return api, nil
	}
	return info.ParseSubscription(data)
}

func (mm *MelodyManager) cleanupSession(s *melody.Session) {
	// 🔧 先在锁外检查
	mm.subscriptionsMu.RLock()
	ss, exists := mm.subscriptions[s]
	mm.subscriptionsMu.RUnlock()

	if !exists {
		return // 快速返回，无需清理
	}

	// 🔧 在锁外执行耗时的清理操作
	if ss != nil {
		// 这里可能是耗时操作，在锁外执行
		go func() {
			defer func() {
				if err := recover(); err != nil {
					logx.Errorf("清理会话时发生错误: %v", err)
				}
			}()
			ss.UnsubscribeAll()
		}()
	}

	// 🔧 最后才在锁内删除映射
	mm.subscriptionsMu.Lock()
	delete(mm.subscriptions, s)
	mm.subscriptionsMu.Unlock()
}
func (mm *MelodyManager) sendToSession(s *melody.Session, event, channel string, data interface{}) {
	if s == nil || s.IsClosed() {
		logx.Debugw("websocket_send_skipped",
			logx.Field("event", event),
			logx.Field("channel", channel),
			logx.Field("reason", "session_closed"),
		)
		return
	}
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
	if s.IsClosed() {
		logx.Errorf("尝试向已关闭的WebSocket连接发送消息: %s", s.Request.RemoteAddr)
		return
	}
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
	return make(map[string]map[int]types.IRouter)
}

// 🔧 获取特定频道的订阅
func (mm *MelodyManager) GetChannelSubscriptions(s *melody.Session, channel string) map[int]types.IRouter {
	return make(map[int]types.IRouter)
}
