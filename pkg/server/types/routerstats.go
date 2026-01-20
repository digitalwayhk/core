package types

import (
	"fmt"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// 🆕 RouterStats 路由统计信息（扩展 WebSocket 统计）
type RouterStats struct {
	// 实时统计
	currentSecond   int64 // 当前秒数（Unix时间戳）
	currentReqCount int64 // 当前秒的请求数
	maxReqPerSecond int64 // 每秒最大请求数
	totalRequests   int64 // 总请求数
	totalErrors     int64 // 总错误数

	// 响应时间统计
	minResponseTime   time.Duration // 最小响应时间
	maxResponseTime   time.Duration // 最大响应时间
	totalResponseTime time.Duration // 总响应时间（用于计算平均值）

	// 缓存统计
	cacheHits   int64 // 缓存命中次数
	cacheMisses int64 // 缓存未命中次数
	cacheSize   int64 // 缓存项数量

	// 每秒请求数历史（保留最近60秒）
	qpsHistory      []int64 // QPS历史记录
	qpsHistoryIndex int     // 当前索引位置

	// 🆕 WebSocket 统计
	wsCurrentConnections   int64 // 当前WebSocket连接数
	wsMaxConnections       int64 // 历史最大连接数
	wsTotalConnections     int64 // 总连接数（累计）
	wsTotalDisconnections  int64 // 总断开数（累计）
	wsTotalMessages        int64 // 总消息数（发送）
	wsCurrentMessages      int64 // 当前秒的消息数
	wsMaxMessagesPerSecond int64 // 每秒最大消息数
	wsTotalBroadcasts      int64 // 总广播次数
	wsTotalErrors          int64 // WebSocket错误数
	wsMessageSizeTotal     int64 // 总消息大小（字节）
	wsAvgMessageSize       int64 // 平均消息大小

	// 🆕 WebSocket 连接质量统计
	wsActiveHashes            int64           // 活跃的hash数量
	wsDeadConnectionsCleaned  int64           // 清理的死连接数
	wsConnectionDurations     []time.Duration // 连接持续时间样本（最近100个）
	wsConnectionDurationIndex int             // 连接持续时间索引

	// 🆕 WebSocket 消息历史（保留最近60秒）
	wsMpsHistory      []int64 // MPS (Messages Per Second) 历史
	wsMpsHistoryIndex int     // 当前索引位置

	mu sync.RWMutex

	// 开始统计时间
	startTime time.Time
}

// 🆕 初始化统计（扩展 WebSocket 支持）
func (own *RouterInfo) initStats() {
	if own.stats != nil {
		return
	}
	own.statsLock.Lock()
	defer own.statsLock.Unlock()
	if own.stats != nil {
		return
	}
	own.stats = &RouterStats{
		currentSecond:         time.Now().Unix(),
		minResponseTime:       time.Hour * 24,             // 初始设置为一个大值
		qpsHistory:            make([]int64, 60),          // 保留60秒历史
		wsMpsHistory:          make([]int64, 60),          // 保留60秒消息历史
		wsConnectionDurations: make([]time.Duration, 100), // 保留100个样本
		startTime:             time.Now(),
	}

	// 启动QPS和WebSocket统计协程
	go own.updateStatsPerSecond()
	// 启动QPS统计协程
	go own.updateQPSStats()

}

// 🆕 更新QPS统计（每秒执行）
func (own *RouterInfo) updateQPSStats() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		own.stats.mu.Lock()

		currentSec := time.Now().Unix()
		if currentSec != own.stats.currentSecond {
			// 保存当前秒的请求数到历史
			own.stats.qpsHistory[own.stats.qpsHistoryIndex] = own.stats.currentReqCount
			own.stats.qpsHistoryIndex = (own.stats.qpsHistoryIndex + 1) % 60

			// 更新最大QPS
			if own.stats.currentReqCount > own.stats.maxReqPerSecond {
				own.stats.maxReqPerSecond = own.stats.currentReqCount
			}

			// 重置当前秒计数
			own.stats.currentSecond = currentSec
			own.stats.currentReqCount = 0
		}

		own.stats.mu.Unlock()
	}
}

// 🆕 记录请求开始
func (own *RouterInfo) recordRequestStart() func() {
	own.initStats()

	startTime := time.Now()

	own.stats.mu.Lock()
	own.stats.currentReqCount++
	own.stats.totalRequests++
	own.stats.mu.Unlock()

	// 返回记录结束的函数
	return func() {
		own.recordRequestEnd(startTime, nil)
	}
}

// 🆕 记录请求结束
func (own *RouterInfo) recordRequestEnd(startTime time.Time, err error) {
	duration := time.Since(startTime)

	own.stats.mu.Lock()
	defer own.stats.mu.Unlock()

	// 更新响应时间统计
	own.stats.totalResponseTime += duration

	if duration < own.stats.minResponseTime {
		own.stats.minResponseTime = duration
	}

	if duration > own.stats.maxResponseTime {
		own.stats.maxResponseTime = duration
	}

	// 错误统计
	if err != nil {
		own.stats.totalErrors++
	}
}

// 🆕 记录缓存命中
func (own *RouterInfo) recordCacheHit() {
	own.stats.mu.Lock()
	own.stats.cacheHits++
	own.stats.mu.Unlock()
}

// 🆕 记录缓存未命中
func (own *RouterInfo) recordCacheMiss() {
	own.stats.mu.Lock()
	own.stats.cacheMisses++
	own.stats.mu.Unlock()
}

// 🆕 更新缓存大小
func (own *RouterInfo) updateCacheSize() {
	count := int64(0)
	own.rCache.Range(func(key, value interface{}) bool {
		count++
		return true
	})

	own.stats.mu.Lock()
	own.stats.cacheSize = count
	own.stats.mu.Unlock()
}

// 🆕 每秒更新统计（合并 QPS 和 WebSocket MPS）
func (own *RouterInfo) updateStatsPerSecond() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		own.stats.mu.Lock()

		currentSec := time.Now().Unix()
		if currentSec != own.stats.currentSecond {
			// 保存 QPS 历史
			own.stats.qpsHistory[own.stats.qpsHistoryIndex] = own.stats.currentReqCount
			own.stats.qpsHistoryIndex = (own.stats.qpsHistoryIndex + 1) % 60

			// 更新最大QPS
			if own.stats.currentReqCount > own.stats.maxReqPerSecond {
				own.stats.maxReqPerSecond = own.stats.currentReqCount
			}

			// 保存 WebSocket MPS 历史
			own.stats.wsMpsHistory[own.stats.wsMpsHistoryIndex] = own.stats.wsCurrentMessages
			own.stats.wsMpsHistoryIndex = (own.stats.wsMpsHistoryIndex + 1) % 60

			// 更新最大MPS
			if own.stats.wsCurrentMessages > own.stats.wsMaxMessagesPerSecond {
				own.stats.wsMaxMessagesPerSecond = own.stats.wsCurrentMessages
			}

			// 重置当前秒计数
			own.stats.currentSecond = currentSec
			own.stats.currentReqCount = 0
			own.stats.wsCurrentMessages = 0
		}

		// 🆕 更新当前WebSocket连接数和活跃hash数
		own.updateWebSocketCurrentStats()

		own.stats.mu.Unlock()
	}
}

// 🆕 更新WebSocket实时统计
func (own *RouterInfo) updateWebSocketCurrentStats() {
	own.websocketlock.RLock()
	defer own.websocketlock.RUnlock()

	// 统计活跃连接数
	activeCount := int64(0)
	for _, clients := range own.rWebSocketClient {
		for ws := range clients {
			if !ws.IsClosed() {
				activeCount++
			}
		}
	}

	own.stats.wsCurrentConnections = activeCount
	own.stats.wsActiveHashes = int64(len(own.rArgs))

	// 更新历史最大连接数
	if activeCount > own.stats.wsMaxConnections {
		own.stats.wsMaxConnections = activeCount
	}
}

// 🆕 记录 WebSocket 连接建立
func (own *RouterInfo) recordWebSocketConnect(hash uint64) {
	own.stats.mu.Lock()
	defer own.stats.mu.Unlock()

	own.stats.wsTotalConnections++
	own.stats.wsCurrentConnections++

	if own.stats.wsCurrentConnections > own.stats.wsMaxConnections {
		own.stats.wsMaxConnections = own.stats.wsCurrentConnections
	}
}

// 🆕 记录 WebSocket 断开连接
func (own *RouterInfo) recordWebSocketDisconnect(hash uint64) {
	own.stats.mu.Lock()
	defer own.stats.mu.Unlock()

	own.stats.wsTotalDisconnections++
	own.stats.wsCurrentConnections--
}

// 🆕 WebSocket 统计详情

// 🆕 记录 WebSocket 消息发送
func (own *RouterInfo) recordWebSocketMessage(messageSize int) {
	own.stats.mu.Lock()
	defer own.stats.mu.Unlock()

	own.stats.wsTotalMessages++
	own.stats.wsCurrentMessages++
	own.stats.wsMessageSizeTotal += int64(messageSize)

	if own.stats.wsTotalMessages > 0 {
		own.stats.wsAvgMessageSize = own.stats.wsMessageSizeTotal / own.stats.wsTotalMessages
	}
}

// 🆕 记录 WebSocket 广播
func (own *RouterInfo) recordWebSocketBroadcast(recipientCount int) {
	own.stats.mu.Lock()
	defer own.stats.mu.Unlock()

	own.stats.wsTotalBroadcasts++
	own.stats.wsTotalMessages += int64(recipientCount)
	own.stats.wsCurrentMessages += int64(recipientCount)
}

// 🆕 记录 WebSocket 错误
func (own *RouterInfo) recordWebSocketError() {
	own.stats.mu.Lock()
	defer own.stats.mu.Unlock()

	own.stats.wsTotalErrors++
}

// 🆕 记录清理的死连接
func (own *RouterInfo) recordDeadConnectionsCleaned(count int) {
	own.stats.mu.Lock()
	defer own.stats.mu.Unlock()

	own.stats.wsDeadConnectionsCleaned += int64(count)
}

// 🆕 RouterStatsSnapshot 扩展 WebSocket 统计
type RouterStatsSnapshot struct {
	// 基本信息
	ServiceName string `json:"service_name"`
	Path        string `json:"path"`
	Method      string `json:"method"`

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

	// QPS历史
	QPSHistory []int64 `json:"qps_history"`

	// 🆕 WebSocket 统计
	WebSocket *WebSocketStats `json:"websocket,omitempty"`

	// 运行时间
	Uptime    string    `json:"uptime"`
	StartTime time.Time `json:"start_time"`
}

type WebSocketStats struct {
	// 连接统计
	CurrentConnections  int64 `json:"current_connections"`  // 当前连接数
	MaxConnections      int64 `json:"max_connections"`      // 历史最大连接数
	TotalConnections    int64 `json:"total_connections"`    // 总连接数（累计）
	TotalDisconnections int64 `json:"total_disconnections"` // 总断开数
	ActiveHashes        int64 `json:"active_hashes"`        // 活跃的hash数

	// 消息统计
	TotalMessages   int64   `json:"total_messages"`   // 总消息数
	CurrentMPS      int64   `json:"current_mps"`      // 当前每秒消息数
	MaxMPS          int64   `json:"max_mps"`          // 最大每秒消息数
	AvgMPS          float64 `json:"avg_mps"`          // 平均每秒消息数
	TotalBroadcasts int64   `json:"total_broadcasts"` // 总广播次数

	// 消息大小统计
	TotalMessageSize int64 `json:"total_message_size_bytes"` // 总消息大小
	AvgMessageSize   int64 `json:"avg_message_size_bytes"`   // 平均消息大小

	// 错误统计
	TotalErrors int64   `json:"total_errors"` // 总错误数
	ErrorRate   float64 `json:"error_rate"`   // 错误率

	// 清理统计
	DeadConnectionsCleaned int64 `json:"dead_connections_cleaned"` // 清理的死连接数

	// MPS历史
	MPSHistory []int64 `json:"mps_history"` // 每秒消息数历史
}

// 🆕 GetStats 扩展 WebSocket 统计
func (own *RouterInfo) GetStats() *RouterStatsSnapshot {
	if own.stats == nil {
		own.initStats()
	}

	own.stats.mu.RLock()
	defer own.stats.mu.RUnlock()

	snapshot := &RouterStatsSnapshot{
		ServiceName:   own.ServiceName,
		Path:          own.Path,
		Method:        own.Method,
		CurrentQPS:    own.stats.currentReqCount,
		MaxQPS:        own.stats.maxReqPerSecond,
		TotalRequests: own.stats.totalRequests,
		TotalErrors:   own.stats.totalErrors,
		CacheHits:     own.stats.cacheHits,
		CacheMisses:   own.stats.cacheMisses,
		CacheSize:     own.stats.cacheSize,
		StartTime:     own.stats.startTime,
		QPSHistory:    make([]int64, 60),
	}

	// 计算平均QPS
	uptime := time.Since(own.stats.startTime).Seconds()
	if uptime > 0 {
		snapshot.AvgQPS = float64(own.stats.totalRequests) / uptime
	}

	// 计算错误率
	if snapshot.TotalRequests > 0 {
		snapshot.ErrorRate = float64(snapshot.TotalErrors) / float64(snapshot.TotalRequests) * 100
	}

	// 计算缓存命中率
	totalCacheAccess := snapshot.CacheHits + snapshot.CacheMisses
	if totalCacheAccess > 0 {
		snapshot.CacheHitRate = float64(snapshot.CacheHits) / float64(totalCacheAccess) * 100
	}

	// 响应时间统计
	if own.stats.minResponseTime < time.Hour*24 {
		snapshot.MinResponseTime = own.stats.minResponseTime.String()
	} else {
		snapshot.MinResponseTime = "N/A"
	}

	snapshot.MaxResponseTime = own.stats.maxResponseTime.String()

	if own.stats.totalRequests > 0 {
		avgDuration := own.stats.totalResponseTime / time.Duration(own.stats.totalRequests)
		snapshot.AvgResponseTime = avgDuration.String()
	} else {
		snapshot.AvgResponseTime = "N/A"
	}

	// 复制QPS历史
	copy(snapshot.QPSHistory, own.stats.qpsHistory)

	// 🆕 WebSocket 统计
	if own.stats.wsTotalConnections > 0 || own.stats.wsCurrentConnections > 0 {
		wsStats := &WebSocketStats{
			CurrentConnections:     own.stats.wsCurrentConnections,
			MaxConnections:         own.stats.wsMaxConnections,
			TotalConnections:       own.stats.wsTotalConnections,
			TotalDisconnections:    own.stats.wsTotalDisconnections,
			ActiveHashes:           own.stats.wsActiveHashes,
			TotalMessages:          own.stats.wsTotalMessages,
			CurrentMPS:             own.stats.wsCurrentMessages,
			MaxMPS:                 own.stats.wsMaxMessagesPerSecond,
			TotalBroadcasts:        own.stats.wsTotalBroadcasts,
			TotalMessageSize:       own.stats.wsMessageSizeTotal,
			AvgMessageSize:         own.stats.wsAvgMessageSize,
			TotalErrors:            own.stats.wsTotalErrors,
			DeadConnectionsCleaned: own.stats.wsDeadConnectionsCleaned,
			MPSHistory:             make([]int64, 60),
		}

		// 计算平均MPS
		if uptime > 0 {
			wsStats.AvgMPS = float64(own.stats.wsTotalMessages) / uptime
		}

		// 计算错误率
		if own.stats.wsTotalMessages > 0 {
			wsStats.ErrorRate = float64(own.stats.wsTotalErrors) / float64(own.stats.wsTotalMessages) * 100
		}

		// 复制MPS历史
		copy(wsStats.MPSHistory, own.stats.wsMpsHistory)

		snapshot.WebSocket = wsStats
	}

	// 运行时长
	snapshot.Uptime = time.Since(own.stats.startTime).Round(time.Second).String()

	return snapshot
}

// 🆕 PrintStats 扩展 WebSocket 统计输出
func (own *RouterInfo) PrintStats() {
	snapshot := own.GetStats()

	wsInfo := ""
	if snapshot.WebSocket != nil {
		ws := snapshot.WebSocket
		wsInfo = fmt.Sprintf(`╠───────────────────────────────────────────────────────────────
║ WebSocket 统计:
║   当前连接:  %d
║   最大连接:  %d
║   总连接数:  %d
║   活跃Hash:  %d
║   当前 MPS:  %d msg/s
║   最大 MPS:  %d msg/s
║   平均 MPS:  %.2f msg/s
║   总消息数:  %d
║   总广播数:  %d
║   平均消息:  %d bytes
║   总错误数:  %d
║   错误率:    %.2f%%
║   清理连接:  %d`,
			ws.CurrentConnections,
			ws.MaxConnections,
			ws.TotalConnections,
			ws.ActiveHashes,
			ws.CurrentMPS,
			ws.MaxMPS,
			ws.AvgMPS,
			ws.TotalMessages,
			ws.TotalBroadcasts,
			ws.AvgMessageSize,
			ws.TotalErrors,
			ws.ErrorRate,
			ws.DeadConnectionsCleaned,
		)
	}

	logx.Infof(`
╔═══════════════════════════════════════════════════════════════
║ 路由统计: %s %s
╠═══════════════════════════════════════════════════════════════
║ 运行时长: %s
║ 开始时间: %s
╠───────────────────────────────────────────────────────────────
║ QPS 统计:
║   当前 QPS:  %d req/s
║   最大 QPS:  %d req/s
║   平均 QPS:  %.2f req/s
╠───────────────────────────────────────────────────────────────
║ 请求统计:
║   总请求数:  %d
║   总错误数:  %d
║   错误率:    %.2f%%
╠───────────────────────────────────────────────────────────────
║ 响应时间:
║   最小:      %s
║   最大:      %s
║   平均:      %s
╠───────────────────────────────────────────────────────────────
║ 缓存统计:
║   命中次数:  %d
║   未命中:    %d
║   命中率:    %.2f%%
║   缓存大小:  %d 项%s
╚═══════════════════════════════════════════════════════════════
`,
		snapshot.ServiceName,
		snapshot.Path,
		snapshot.Uptime,
		snapshot.StartTime.Format("2006-01-02 15:04:05"),
		snapshot.CurrentQPS,
		snapshot.MaxQPS,
		snapshot.AvgQPS,
		snapshot.TotalRequests,
		snapshot.TotalErrors,
		snapshot.ErrorRate,
		snapshot.MinResponseTime,
		snapshot.MaxResponseTime,
		snapshot.AvgResponseTime,
		snapshot.CacheHits,
		snapshot.CacheMisses,
		snapshot.CacheHitRate,
		snapshot.CacheSize,
		wsInfo,
	)
}
