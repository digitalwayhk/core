package router

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/zeromicro/go-zero/core/logx"
)

// StatsManager 统计管理器
type StatsManager struct {
	routers     []*types.RouterInfo
	serviceName string
}

// AggregatedStats 聚合统计信息
type AggregatedStats struct {
	ServiceName    string `json:"service_name"`    // 服务名称
	TotalRouters   int    `json:"total_routers"`   // 总路由数
	PublicRouters  int    `json:"public_routers"`  // 公共路由数
	PrivateRouters int    `json:"private_routers"` // 私有路由数
	ManageRouters  int    `json:"manage_routers"`  // 管理路由数

	// 汇总统计
	TotalRequests    int64   `json:"total_requests"`     // 总请求数
	TotalErrors      int64   `json:"total_errors"`       // 总错误数
	TotalQPS         int64   `json:"total_qps"`          // 总当前QPS
	AvgQPS           float64 `json:"avg_qps"`            // 平均QPS
	MaxQPS           int64   `json:"max_qps"`            // 最大QPS
	TotalCacheHits   int64   `json:"total_cache_hits"`   // 总缓存命中
	TotalCacheMisses int64   `json:"total_cache_misses"` // 总缓存未命中

	// WebSocket 汇总
	TotalWSConnections int64 `json:"total_ws_connections"` // 总WebSocket连接
	TotalWSMessages    int64 `json:"total_ws_messages"`    // 总WebSocket消息
	TotalWSErrors      int64 `json:"total_ws_errors"`      // 总WebSocket错误

	// 详细列表
	Routers []*types.RouterStatsSnapshot `json:"routers"` // 路由详情

	CollectedAt time.Time `json:"collected_at"` // 收集时间
}

// SortField 排序字段
type SortField string

const (
	SortByPath            SortField = "path"
	SortByQPS             SortField = "qps"
	SortByMaxQPS          SortField = "max_qps"
	SortByAvgQPS          SortField = "avg_qps"
	SortByTotalRequests   SortField = "total_requests"
	SortByTotalErrors     SortField = "total_errors"
	SortByErrorRate       SortField = "error_rate"
	SortByAvgResponseTime SortField = "avg_response_time"
	SortByCacheHitRate    SortField = "cache_hit_rate"
	SortByWSConnections   SortField = "ws_connections"
	SortByWSMessages      SortField = "ws_messages"
	SortByWSMPS           SortField = "ws_mps"
)

// SortOrder 排序方向
type SortOrder string

const (
	SortAsc  SortOrder = "asc"  // 升序
	SortDesc SortOrder = "desc" // 降序
)

// NewStatsManager 创建统计管理器
func NewStatsManager(serviceName string, routers []*types.RouterInfo) *StatsManager {
	return &StatsManager{
		serviceName: serviceName,
		routers:     routers,
	}
}

// GetAllStats 获取所有路由统计（可过滤和排序）
func (sm *StatsManager) GetAllStats(
	filterTypes []types.ApiType,
	sortBy SortField,
	order SortOrder,
) *AggregatedStats {
	stats := &AggregatedStats{
		ServiceName: sm.serviceName,
		Routers:     make([]*types.RouterStatsSnapshot, 0),
		CollectedAt: time.Now(),
	}

	// 收集统计信息
	for _, router := range sm.routers {
		// 过滤路由类型
		if len(filterTypes) > 0 {
			matched := false
			for _, t := range filterTypes {
				if router.PathType == t {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		snapshot := router.GetStats()
		stats.Routers = append(stats.Routers, snapshot)

		// 统计路由类型
		switch router.PathType {
		case types.PublicType:
			stats.PublicRouters++
		case types.PrivateType:
			stats.PrivateRouters++
		case types.ManageType:
			stats.ManageRouters++
		}

		// 累加统计数据
		stats.TotalRequests += snapshot.TotalRequests
		stats.TotalErrors += snapshot.TotalErrors
		stats.TotalQPS += snapshot.CurrentQPS
		stats.TotalCacheHits += snapshot.CacheHits
		stats.TotalCacheMisses += snapshot.CacheMisses

		if snapshot.MaxQPS > stats.MaxQPS {
			stats.MaxQPS = snapshot.MaxQPS
		}

		// WebSocket 统计
		if snapshot.WebSocket != nil {
			stats.TotalWSConnections += int64(snapshot.WebSocket.CurrentConnections)
			stats.TotalWSMessages += snapshot.WebSocket.TotalMessages
			stats.TotalWSErrors += snapshot.WebSocket.TotalErrors
		}
	}

	stats.TotalRouters = len(stats.Routers)

	// 计算平均QPS
	if stats.TotalRouters > 0 {
		stats.AvgQPS = float64(stats.TotalQPS) / float64(stats.TotalRouters)
	}

	// 排序
	sm.sortStats(stats.Routers, sortBy, order)

	return stats
}

// sortStats 排序统计列表
func (sm *StatsManager) sortStats(
	routers []*types.RouterStatsSnapshot,
	sortBy SortField,
	order SortOrder,
) {
	sort.Slice(routers, func(i, j int) bool {
		var result bool

		switch sortBy {
		case SortByPath:
			result = routers[i].Path < routers[j].Path

		case SortByQPS:
			result = routers[i].CurrentQPS < routers[j].CurrentQPS

		case SortByMaxQPS:
			result = routers[i].MaxQPS < routers[j].MaxQPS

		case SortByAvgQPS:
			result = routers[i].AvgQPS < routers[j].AvgQPS

		case SortByTotalRequests:
			result = routers[i].TotalRequests < routers[j].TotalRequests

		case SortByTotalErrors:
			result = routers[i].TotalErrors < routers[j].TotalErrors

		case SortByErrorRate:
			result = routers[i].ErrorRate < routers[j].ErrorRate

		case SortByAvgResponseTime:
			// 解析响应时间字符串进行比较
			durI := parseResponseTime(routers[i].AvgResponseTime)
			durJ := parseResponseTime(routers[j].AvgResponseTime)
			result = durI < durJ

		case SortByCacheHitRate:
			result = routers[i].CacheHitRate < routers[j].CacheHitRate

		case SortByWSConnections:
			connI := int64(0)
			connJ := int64(0)
			if routers[i].WebSocket != nil {
				connI = routers[i].WebSocket.CurrentConnections
			}
			if routers[j].WebSocket != nil {
				connJ = routers[j].WebSocket.CurrentConnections
			}
			result = connI < connJ

		case SortByWSMessages:
			msgI := int64(0)
			msgJ := int64(0)
			if routers[i].WebSocket != nil {
				msgI = routers[i].WebSocket.TotalMessages
			}
			if routers[j].WebSocket != nil {
				msgJ = routers[j].WebSocket.TotalMessages
			}
			result = msgI < msgJ

		case SortByWSMPS:
			mpsI := int64(0)
			mpsJ := int64(0)
			if routers[i].WebSocket != nil {
				mpsI = routers[i].WebSocket.CurrentMPS
			}
			if routers[j].WebSocket != nil {
				mpsJ = routers[j].WebSocket.CurrentMPS
			}
			result = mpsI < mpsJ

		default:
			result = routers[i].Path < routers[j].Path
		}

		// 根据排序方向返回结果
		if order == SortDesc {
			return !result
		}
		return result
	})
}

// parseResponseTime 解析响应时间字符串
func parseResponseTime(s string) time.Duration {
	if s == "N/A" {
		return time.Duration(0)
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return time.Duration(0)
	}
	return d
}

// GetTopN 获取排名前N的路由
func (sm *StatsManager) GetTopN(
	n int,
	filterTypes []types.ApiType,
	sortBy SortField,
) []*types.RouterStatsSnapshot {
	allStats := sm.GetAllStats(filterTypes, sortBy, SortDesc)

	if len(allStats.Routers) <= n {
		return allStats.Routers
	}

	return allStats.Routers[:n]
}

// GetStatsJSON 获取JSON格式的统计信息
func (sm *StatsManager) GetStatsJSON(
	filterTypes []types.ApiType,
	sortBy SortField,
	order SortOrder,
) string {
	stats := sm.GetAllStats(filterTypes, sortBy, order)
	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"error": "%s"}`, err.Error())
	}
	return string(data)
}

// PrintTopStats 打印排名统计
func (sm *StatsManager) PrintTopStats(
	n int,
	filterTypes []types.ApiType,
	sortBy SortField,
) {
	topRouters := sm.GetTopN(n, filterTypes, sortBy)

	typeFilter := "全部"
	if len(filterTypes) > 0 {
		types := make([]string, len(filterTypes))
		for i, t := range filterTypes {
			types[i] = string(t)
		}
		typeFilter = strings.Join(types, ", ")
	}

	logx.Infow("router_top_stats",
		logx.Field("limit", n),
		logx.Field("api_types", typeFilter),
		logx.Field("sort", sortBy),
		logx.Field("result_count", len(topRouters)),
	)

	for idx, router := range topRouters {
		websocketConnections := int64(0)
		websocketMPS := int64(0)
		if router.WebSocket != nil {
			websocketConnections = router.WebSocket.CurrentConnections
			websocketMPS = router.WebSocket.CurrentMPS
		}

		logx.Debugw("router_top_stat",
			logx.Field("rank", idx+1),
			logx.Field("route", router.Path),
			logx.Field("current_qps", router.CurrentQPS),
			logx.Field("max_qps", router.MaxQPS),
			logx.Field("average_qps", router.AvgQPS),
			logx.Field("total_requests", router.TotalRequests),
			logx.Field("error_rate", router.ErrorRate),
			logx.Field("websocket_connections", websocketConnections),
			logx.Field("websocket_mps", websocketMPS),
		)
	}
}

// GetSummary 获取统计摘要
func (sm *StatsManager) GetSummary(filterTypes []types.ApiType) string {
	stats := sm.GetAllStats(filterTypes, SortByPath, SortAsc)

	typeFilter := "全部"
	if len(filterTypes) > 0 {
		types := make([]string, len(filterTypes))
		for i, t := range filterTypes {
			types[i] = string(t)
		}
		typeFilter = strings.Join(types, ", ")
	}

	return fmt.Sprintf(`
╔═══════════════════════════════════════════════════════════════
 路由统计摘要 [类型: %s]
╠═══════════════════════════════════════════════════════════════
 路由总数:     %d
   - Public:   %d
   - Private:  %d
   - Manage:   %d
╠───────────────────────────────────────────────────────────────
 HTTP 统计:
   总请求数:   %d
   总错误数:   %d
   当前 QPS:   %d
   平均 QPS:   %.2f
   最大 QPS:   %d
   缓存命中:   %d
   缓存未命中: %d
╠───────────────────────────────────────────────────────────────
 WebSocket 统计:
   总连接数:   %d
   总消息数:   %d
   总错误数:   %d
╠───────────────────────────────────────────────────────────────
 收集时间:     %s
═══════════════════════════════════════════════════════════════`,
		typeFilter,
		stats.TotalRouters,
		stats.PublicRouters,
		stats.PrivateRouters,
		stats.ManageRouters,
		stats.TotalRequests,
		stats.TotalErrors,
		stats.TotalQPS,
		stats.AvgQPS,
		stats.MaxQPS,
		stats.TotalCacheHits,
		stats.TotalCacheMisses,
		stats.TotalWSConnections,
		stats.TotalWSMessages,
		stats.TotalWSErrors,
		stats.CollectedAt.Format("2006-01-02 15:04:05"),
	)
}
