package types

import (
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	//  统计开关（调试时可设为 false）
	enableStats = false
)

// RouterStats 路由统计信息。
//
// Deprecated: 旧内存统计链路已关闭（enableStats=false），不再作为生产指标源。
// 请使用 go-zero/Prometheus + Runtime API（runtimetopology/runtimeservice）。
// 见 docs/codex/DEPRECATION_REGISTER.md。
type RouterStats struct {
	Path        string
	ServiceName string
	StartTime   time.Time
	closeChan   chan struct{}

	// QPS 统计
	Request *RequestStats

	// 缓存统计
	Cache *CacheStats

	// WebSocket 统计
	WebSocket *WebSocketStats

	mu sync.RWMutex
}

// RequestStats QPS统计
type RequestStats struct {
	CurrentSecond   int64            // 当前秒数（Unix时间戳）
	CurrentReqCount int64            // 当前秒的请求数
	MaxReqPerSecond int64            // 每秒最大请求数
	TotalRequests   int64            // 总请求数
	TotalErrors     int64            // 总错误数
	History         []RequestHistory // 历史记录（最近60秒）
	HistoryIndex    int              // 当前索引位置

	// 响应时间统计
	MinResponseTime   time.Duration // 最小响应时间
	MaxResponseTime   time.Duration // 最大响应时间
	TotalResponseTime time.Duration // 总响应时间
}

type RequestHistory struct {
	Timestamp time.Time
	Count     int64
	AvgTime   time.Duration
}

// CacheStats 缓存统计
type CacheStats struct {
	Hits   int64 // 缓存命中次数
	Misses int64 // 缓存未命中次数
	Size   int64 // 缓存项数量
}

// WebSocketStats WebSocket统计
type WebSocketStats struct {
	// 连接统计
	CurrentConnections  int64 `json:"current_connections"`
	MaxConnections      int64 `json:"max_connections"`
	TotalConnections    int64 `json:"total_connections"`
	TotalDisconnections int64 `json:"total_disconnections"`
	TotalRegistered     int64 `json:"total_registered"` // 总注册数（包括死连接）
	// 消息统计
	TotalMessages   int64 `json:"total_messages"`
	CurrentMPS      int64 `json:"current_mps"` // 当前每秒消息数
	MaxMPS          int64 `json:"max_mps"`
	TotalBroadcasts int64 `json:"total_broadcasts"`

	// 消息大小统计
	TotalMessageSize int64 `json:"total_message_size_bytes"`
	AvgMessageSize   int64 `json:"avg_message_size_bytes"`

	// 错误统计
	TotalErrors int64 `json:"total_errors"`

	// 清理统计
	DeadConnectionsCleaned int64 `json:"dead_connections_cleaned"`

	// Hash统计
	ConnectionsByHash map[uint64]int `json:"connections_by_hash"`

	// MPS历史
	MPSHistory      []int64 `json:"mps_history"`
	MPSHistoryIndex int     `json:"-"`

	mu sync.RWMutex
}

// 初始化统计
func (own *RouterInfo) initStats() {
	if !enableStats {
		return //  直接返回，不初始化
	}
	own.statsLock.Lock()
	defer own.statsLock.Unlock()

	//  防止重复初始化
	if own.stats != nil {
		return
	}

	own.stats = &RouterStats{
		Path:        own.Path,
		ServiceName: own.ServiceName,
		StartTime:   time.Now(),
		closeChan:   make(chan struct{}),

		Request: &RequestStats{
			History:         make([]RequestHistory, 0, 60),
			MinResponseTime: time.Hour * 24, // 初始化为一个大值
		},

		Cache: &CacheStats{},

		WebSocket: &WebSocketStats{
			ConnectionsByHash: make(map[uint64]int),
			MPSHistory:        make([]int64, 60),
		},
	}

	//  启动统计协程
	go own.updateStatsPerSecond()

	logx.Infof(" 统计系统已启动: %s", own.Path)
}

// 关闭统计系统
func (own *RouterInfo) closeStats() {
	if !enableStats {
		return //  直接返回，不初始化
	}
	own.statsLock.Lock()
	defer own.statsLock.Unlock()

	if own.stats == nil {
		return
	}

	//  安全地关闭通道
	select {
	case <-own.stats.closeChan:
		// 已经关闭
	default:
		close(own.stats.closeChan)
	}

	logx.Infof(" 统计系统已关闭: %s", own.Path)
}

// 获取关闭通道（防止 panic）
func (own *RouterInfo) getStatsCloseChan() chan struct{} {
	own.statsLock.RLock()
	defer own.statsLock.RUnlock()

	if own.stats == nil {
		// 返回一个永远阻塞的通道
		ch := make(chan struct{})
		return ch
	}
	return own.stats.closeChan
}

// 每秒更新统计
func (own *RouterInfo) updateStatsPerSecond() {
	if !enableStats {
		return //  直接返回，不初始化
	}
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			func() {
				defer func() {
					if err := recover(); err != nil {
						logx.Errorf("更新统计时发生错误: %v", err)
					}
				}()

				own.updateRequestStats()
				own.updateCacheStats()
				own.updateWebSocketStats()
			}()

		case <-own.getStatsCloseChan():
			logx.Infof("统计协程退出: %s", own.Path)
			return
		}
	}
}

// 更新请求统计
func (own *RouterInfo) updateRequestStats() {
	own.statsLock.RLock()
	if own.stats == nil || own.stats.Request == nil {
		own.statsLock.RUnlock()
		return
	}
	own.statsLock.RUnlock()

	own.stats.mu.Lock()
	defer own.stats.mu.Unlock()

	currentSec := time.Now().Unix()
	if currentSec != own.stats.Request.CurrentSecond {
		// 保存当前秒的统计到历史
		if own.stats.Request.CurrentReqCount > 0 {
			avgTime := time.Duration(0)
			if own.stats.Request.TotalRequests > 0 {
				avgTime = own.stats.Request.TotalResponseTime / time.Duration(own.stats.Request.TotalRequests)
			}

			history := RequestHistory{
				Timestamp: time.Unix(own.stats.Request.CurrentSecond, 0),
				Count:     own.stats.Request.CurrentReqCount,
				AvgTime:   avgTime,
			}

			if len(own.stats.Request.History) < 60 {
				own.stats.Request.History = append(own.stats.Request.History, history)
			} else {
				own.stats.Request.History[own.stats.Request.HistoryIndex] = history
			}
			own.stats.Request.HistoryIndex = (own.stats.Request.HistoryIndex + 1) % 60

			// 更新最大QPS
			if own.stats.Request.CurrentReqCount > own.stats.Request.MaxReqPerSecond {
				own.stats.Request.MaxReqPerSecond = own.stats.Request.CurrentReqCount
			}
		}

		// 重置当前秒计数
		own.stats.Request.CurrentSecond = currentSec
		own.stats.Request.CurrentReqCount = 0
	}
}

// 更新缓存统计
func (own *RouterInfo) updateCacheStats() {
	own.statsLock.RLock()
	if own.stats == nil || own.stats.Cache == nil {
		own.statsLock.RUnlock()
		return
	}
	own.statsLock.RUnlock()

	own.stats.mu.Lock()
	own.stats.Cache.Size = 0
	own.stats.mu.Unlock()
}

// 更新WebSocket统计
func (own *RouterInfo) updateWebSocketStats() {
	own.statsLock.RLock()
	if own.stats == nil || own.stats.WebSocket == nil {
		own.statsLock.RUnlock()
		return
	}
	own.statsLock.RUnlock()

	own.RLock()
	hub := own.webSocketHub
	own.RUnlock()
	counts := make(map[uint64]int)
	var activeClients int64
	if hub != nil {
		counts = hub.hashClientCounts(own)
		for _, count := range counts {
			activeClients += int64(count)
		}
	}

	//  更新统计
	own.stats.WebSocket.mu.Lock()
	defer own.stats.WebSocket.mu.Unlock()

	own.stats.WebSocket.CurrentConnections = activeClients
	own.stats.WebSocket.TotalRegistered = activeClients
	own.stats.WebSocket.ConnectionsByHash = counts

	// 更新最大连接数
	if int64(activeClients) > own.stats.WebSocket.MaxConnections {
		own.stats.WebSocket.MaxConnections = int64(activeClients)
	}

	// 更新 MPS 历史
	if own.stats.WebSocket.CurrentMPS > own.stats.WebSocket.MaxMPS {
		own.stats.WebSocket.MaxMPS = own.stats.WebSocket.CurrentMPS
	}

	own.stats.WebSocket.MPSHistory[own.stats.WebSocket.MPSHistoryIndex] = own.stats.WebSocket.CurrentMPS
	own.stats.WebSocket.MPSHistoryIndex = (own.stats.WebSocket.MPSHistoryIndex + 1) % 60
	own.stats.WebSocket.CurrentMPS = 0 // 重置当前秒的消息数
}

// ==================== 记录方法 ====================

// 记录请求开始
func (own *RouterInfo) recordRequestStart() func() {
	if !enableStats || own.stats == nil {
		return func() {}
	}
	startTime := time.Now()

	own.stats.mu.Lock()
	own.stats.Request.CurrentReqCount++
	own.stats.Request.TotalRequests++
	own.stats.mu.Unlock()

	// 返回记录结束的函数
	return func() {
		own.recordRequestEnd(startTime, nil)
	}
}

// 记录请求结束
func (own *RouterInfo) recordRequestEnd(startTime time.Time, err error) {
	if !enableStats || own.stats == nil {
		return
	}

	duration := time.Since(startTime)

	own.stats.mu.Lock()
	defer own.stats.mu.Unlock()

	// 更新响应时间统计
	own.stats.Request.TotalResponseTime += duration

	if duration < own.stats.Request.MinResponseTime {
		own.stats.Request.MinResponseTime = duration
	}

	if duration > own.stats.Request.MaxResponseTime {
		own.stats.Request.MaxResponseTime = duration
	}

	// 错误统计
	if err != nil {
		own.stats.Request.TotalErrors++
	}
}

// 记录缓存命中
func (own *RouterInfo) recordCacheHit() {
	if !enableStats || own.stats == nil {
		return
	}
	if !enableStats {
		return //  直接返回，不初始化
	}
	own.stats.mu.Lock()
	own.stats.Cache.Hits++
	own.stats.mu.Unlock()
}

// 记录缓存未命中
func (own *RouterInfo) recordCacheMiss() {
	if !enableStats || own.stats == nil {
		return
	}
	if !enableStats {
		return //  直接返回，不初始化
	}
	own.stats.mu.Lock()
	own.stats.Cache.Misses++
	own.stats.mu.Unlock()
}

// 记录 WebSocket 连接建立
func (own *RouterInfo) recordWebSocketConnect(hash uint64) {
	if !enableStats || own.stats == nil {
		return
	}
	own.stats.WebSocket.mu.Lock()
	defer own.stats.WebSocket.mu.Unlock()

	own.stats.WebSocket.TotalConnections++
	own.stats.WebSocket.CurrentConnections++

	if int64(own.stats.WebSocket.CurrentConnections) > own.stats.WebSocket.MaxConnections {
		own.stats.WebSocket.MaxConnections = int64(own.stats.WebSocket.CurrentConnections)
	}

	// 更新hash统计
	own.stats.WebSocket.ConnectionsByHash[hash]++
}

// 记录 WebSocket 断开连接
func (own *RouterInfo) recordWebSocketDisconnect(hash uint64) {
	if !enableStats || own.stats == nil {
		return
	}

	own.stats.WebSocket.mu.Lock()
	defer own.stats.WebSocket.mu.Unlock()

	own.stats.WebSocket.TotalDisconnections++
	if own.stats.WebSocket.CurrentConnections > 0 {
		own.stats.WebSocket.CurrentConnections--
	}

	// 更新hash统计
	if count, ok := own.stats.WebSocket.ConnectionsByHash[hash]; ok && count > 0 {
		own.stats.WebSocket.ConnectionsByHash[hash]--
		if own.stats.WebSocket.ConnectionsByHash[hash] == 0 {
			delete(own.stats.WebSocket.ConnectionsByHash, hash)
		}
	}
}

// 记录 WebSocket 消息发送
func (own *RouterInfo) recordWebSocketMessage(messageSize int) {
	if !enableStats || own.stats == nil {
		return
	}

	own.stats.WebSocket.mu.Lock()
	defer own.stats.WebSocket.mu.Unlock()

	own.stats.WebSocket.TotalMessages++
	own.stats.WebSocket.CurrentMPS++
	own.stats.WebSocket.TotalMessageSize += int64(messageSize)

	if own.stats.WebSocket.TotalMessages > 0 {
		own.stats.WebSocket.AvgMessageSize = own.stats.WebSocket.TotalMessageSize / own.stats.WebSocket.TotalMessages
	}
}

// 记录 WebSocket 广播
func (own *RouterInfo) recordWebSocketBroadcast(recipientCount int) {
	if !enableStats || own.stats == nil {
		return
	}

	own.stats.WebSocket.mu.Lock()
	defer own.stats.WebSocket.mu.Unlock()

	own.stats.WebSocket.TotalBroadcasts++
	own.stats.WebSocket.TotalMessages += int64(recipientCount)
	own.stats.WebSocket.CurrentMPS += int64(recipientCount)
}

// 记录 WebSocket 错误
func (own *RouterInfo) recordWebSocketError() {
	if !enableStats || own.stats == nil {
		return
	}

	own.stats.WebSocket.mu.Lock()
	defer own.stats.WebSocket.mu.Unlock()

	own.stats.WebSocket.TotalErrors++
}

// 记录清理的死连接
func (own *RouterInfo) recordDeadConnectionsCleaned(count int) {
	if !enableStats || own.stats == nil {
		return
	}

	own.stats.WebSocket.mu.Lock()
	defer own.stats.WebSocket.mu.Unlock()

	own.stats.WebSocket.DeadConnectionsCleaned += int64(count)
}

// ==================== 获取统计快照 ====================

// RouterStatsSnapshot 统计快照
type RouterStatsSnapshot struct {
	// 基本信息
	ServiceName string `json:"service_name"`
	Path        string `json:"path"`

	// QPS统计
	CurrentQPS int64   `json:"current_qps"`
	MaxQPS     int64   `json:"max_qps"`
	AvgQPS     float64 `json:"avg_qps"`

	// 请求统计
	TotalRequests int64   `json:"total_requests"`
	TotalErrors   int64   `json:"total_errors"`
	ErrorRate     float64 `json:"error_rate"`

	// 响应时间统计
	MinResponseTime string `json:"min_response_time"`
	MaxResponseTime string `json:"max_response_time"`
	AvgResponseTime string `json:"avg_response_time"`

	// 缓存统计
	CacheHits    int64   `json:"cache_hits"`
	CacheMisses  int64   `json:"cache_misses"`
	CacheHitRate float64 `json:"cache_hit_rate"`
	CacheSize    int64   `json:"cache_size"`

	// WebSocket 统计
	WebSocket *WebSocketStatsSnapshot `json:"websocket,omitempty"`

	// 运行时间
	Uptime    string    `json:"uptime"`
	StartTime time.Time `json:"start_time"`
}

type WebSocketStatsSnapshot struct {
	CurrentConnections     int64   `json:"current_connections"`
	MaxConnections         int64   `json:"max_connections"`
	TotalConnections       int64   `json:"total_connections"`
	TotalDisconnections    int64   `json:"total_disconnections"`
	TotalMessages          int64   `json:"total_messages"`
	CurrentMPS             int64   `json:"current_mps"`
	MaxMPS                 int64   `json:"max_mps"`
	AvgMPS                 float64 `json:"avg_mps"`
	TotalBroadcasts        int64   `json:"total_broadcasts"`
	AvgMessageSize         int64   `json:"avg_message_size_bytes"`
	TotalErrors            int64   `json:"total_errors"`
	ErrorRate              float64 `json:"error_rate"`
	DeadConnectionsCleaned int64   `json:"dead_connections_cleaned"`
}

// GetStats 获取统计快照
func (own *RouterInfo) GetStats() *RouterStatsSnapshot {
	if own.stats == nil || !enableStats {
		return nil
	}
	own.stats.mu.RLock()
	defer own.stats.mu.RUnlock()

	snapshot := &RouterStatsSnapshot{
		ServiceName: own.ServiceName,
		Path:        own.Path,
		StartTime:   own.stats.StartTime,
	}

	// 请求统计
	if own.stats.Request != nil {
		snapshot.CurrentQPS = own.stats.Request.CurrentReqCount
		snapshot.MaxQPS = own.stats.Request.MaxReqPerSecond
		snapshot.TotalRequests = own.stats.Request.TotalRequests
		snapshot.TotalErrors = own.stats.Request.TotalErrors

		// 计算平均QPS
		uptime := time.Since(own.stats.StartTime).Seconds()
		if uptime > 0 {
			snapshot.AvgQPS = float64(own.stats.Request.TotalRequests) / uptime
		}

		// 计算错误率
		if snapshot.TotalRequests > 0 {
			snapshot.ErrorRate = float64(snapshot.TotalErrors) / float64(snapshot.TotalRequests) * 100
		}

		// 响应时间统计
		if own.stats.Request.MinResponseTime < time.Hour*24 {
			snapshot.MinResponseTime = own.stats.Request.MinResponseTime.String()
		} else {
			snapshot.MinResponseTime = "N/A"
		}

		snapshot.MaxResponseTime = own.stats.Request.MaxResponseTime.String()

		if own.stats.Request.TotalRequests > 0 {
			avgDuration := own.stats.Request.TotalResponseTime / time.Duration(own.stats.Request.TotalRequests)
			snapshot.AvgResponseTime = avgDuration.String()
		} else {
			snapshot.AvgResponseTime = "N/A"
		}
	}

	// 缓存统计
	if own.stats.Cache != nil {
		snapshot.CacheHits = own.stats.Cache.Hits
		snapshot.CacheMisses = own.stats.Cache.Misses
		snapshot.CacheSize = own.stats.Cache.Size

		totalCacheAccess := snapshot.CacheHits + snapshot.CacheMisses
		if totalCacheAccess > 0 {
			snapshot.CacheHitRate = float64(snapshot.CacheHits) / float64(totalCacheAccess) * 100
		}
	}

	// WebSocket 统计
	if own.stats.WebSocket != nil {
		own.stats.WebSocket.mu.RLock()

		wsSnapshot := &WebSocketStatsSnapshot{
			CurrentConnections:     own.stats.WebSocket.CurrentConnections,
			MaxConnections:         own.stats.WebSocket.MaxConnections,
			TotalConnections:       own.stats.WebSocket.TotalConnections,
			TotalDisconnections:    own.stats.WebSocket.TotalDisconnections,
			TotalMessages:          own.stats.WebSocket.TotalMessages,
			CurrentMPS:             own.stats.WebSocket.CurrentMPS,
			MaxMPS:                 own.stats.WebSocket.MaxMPS,
			TotalBroadcasts:        own.stats.WebSocket.TotalBroadcasts,
			AvgMessageSize:         own.stats.WebSocket.AvgMessageSize,
			TotalErrors:            own.stats.WebSocket.TotalErrors,
			DeadConnectionsCleaned: own.stats.WebSocket.DeadConnectionsCleaned,
		}

		// 计算平均MPS
		uptime := time.Since(own.stats.StartTime).Seconds()
		if uptime > 0 {
			wsSnapshot.AvgMPS = float64(own.stats.WebSocket.TotalMessages) / uptime
		}

		// 计算错误率
		if own.stats.WebSocket.TotalMessages > 0 {
			wsSnapshot.ErrorRate = float64(own.stats.WebSocket.TotalErrors) / float64(own.stats.WebSocket.TotalMessages) * 100
		}

		own.stats.WebSocket.mu.RUnlock()
		snapshot.WebSocket = wsSnapshot
	}

	// 运行时长
	snapshot.Uptime = time.Since(own.stats.StartTime).Round(time.Second).String()

	return snapshot
}

// PrintStats 打印统计信息
func (own *RouterInfo) PrintStats() {
	if !enableStats {
		return //  直接返回，不初始化
	}
	snapshot := own.GetStats()
	websocketConnections := int64(0)
	websocketMessages := int64(0)
	websocketErrors := int64(0)
	if snapshot.WebSocket != nil {
		ws := snapshot.WebSocket
		websocketConnections = ws.CurrentConnections
		websocketMessages = ws.TotalMessages
		websocketErrors = ws.TotalErrors
	}

	logx.Infow("router_stats",
		logx.Field("service", snapshot.ServiceName),
		logx.Field("route", snapshot.Path),
		logx.Field("uptime", snapshot.Uptime),
		logx.Field("current_qps", snapshot.CurrentQPS),
		logx.Field("max_qps", snapshot.MaxQPS),
		logx.Field("average_qps", snapshot.AvgQPS),
		logx.Field("total_requests", snapshot.TotalRequests),
		logx.Field("total_errors", snapshot.TotalErrors),
		logx.Field("error_rate", snapshot.ErrorRate),
		logx.Field("min_response_time", snapshot.MinResponseTime),
		logx.Field("max_response_time", snapshot.MaxResponseTime),
		logx.Field("average_response_time", snapshot.AvgResponseTime),
		logx.Field("cache_hits", snapshot.CacheHits),
		logx.Field("cache_misses", snapshot.CacheMisses),
		logx.Field("cache_hit_rate", snapshot.CacheHitRate),
		logx.Field("cache_size", snapshot.CacheSize),
		logx.Field("websocket_connections", websocketConnections),
		logx.Field("websocket_messages", websocketMessages),
		logx.Field("websocket_errors", websocketErrors),
	)
}
