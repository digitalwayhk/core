// 本文件在订单服务内定时执行 models/stats 声明的 Spec，结果写入 stats.Store。
// API 只读 Store，不得直接查 Order 表做聚合。
package business

import (
	"context"
	"sync"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	// 触发 order_stats 注册
	_ "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/stats"
	"github.com/digitalwayhk/core/pkg/persistence/entity/stats"
	"github.com/zeromicro/go-zero/core/logx"
)

// OrderStatsStore 进程内订单统计快照（API 只读）。
var OrderStatsStore = stats.NewStore()

// SharedStatsRunner 服务默认 runner（Start/Stop 与手动 Refresh 共用）。
var SharedStatsRunner = NewStatsRunner()

// StatsRunner 定时刷新已注册的 order.* 统计。
type StatsRunner struct {
	mu       sync.Mutex
	cancel   context.CancelFunc
	done     chan struct{}
	interval time.Duration
	// lookback 刷新窗口：从 now-lookback 到 now
	lookback time.Duration
}

// NewStatsRunner 创建 runner；默认每 2 分钟刷新最近 90 天。
func NewStatsRunner() *StatsRunner {
	return &StatsRunner{
		interval: 2 * time.Minute,
		lookback: 90 * 24 * time.Hour,
	}
}

// Start 立即跑一轮并启动 ticker。
func (r *StatsRunner) Start() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	r.cancel = cancel
	r.done = done
	interval := r.interval
	if interval <= 0 {
		interval = 2 * time.Minute
	}
	go func() {
		defer close(done)
		r.refreshOnce(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.refreshOnce(ctx)
			}
		}
	}()
}

// Stop 停止 runner。
func (r *StatsRunner) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	cancel := r.cancel
	done := r.done
	r.cancel = nil
	r.done = nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// RefreshNow 同步刷新全部 order.* Spec（供管理 API 手动触发）。
func (r *StatsRunner) RefreshNow(ctx context.Context) []stats.Snapshot {
	return r.refreshOnce(ctx)
}

func (r *StatsRunner) refreshOnce(ctx context.Context) []stats.Snapshot {
	if ctx == nil {
		ctx = context.Background()
	}
	lookback := r.lookback
	if lookback <= 0 {
		lookback = 90 * 24 * time.Hour
	}
	now := time.Now().UTC()
	opt := stats.ExecOptions{
		Range: stats.QueryRange{
			From: now.Add(-lookback),
			To:   now.Add(time.Second),
		},
	}
	action := models.RemoteDataAction()
	out := make([]stats.Snapshot, 0)
	for _, spec := range stats.All() {
		if err := ctx.Err(); err != nil {
			logx.Errorw("order_stats_refresh_aborted",
				logx.Field("error", err),
			)
			break
		}
		// 仅处理本服务注册的 order. 前缀
		if len(spec.Code) < 6 || spec.Code[:6] != "order." {
			continue
		}
		snap, err := stats.Refresh(ctx, OrderStatsStore, action, spec, opt)
		if err != nil {
			logx.Errorw("order_stats_refresh_failed",
				logx.Field("code", spec.Code),
				logx.Field("error", err),
			)
		} else {
			logx.Infow("order_stats_refreshed",
				logx.Field("code", spec.Code),
				logx.Field("rows", len(snap.Rows)),
				logx.Field("computedAt", snap.ComputedAt),
			)
		}
		out = append(out, snap)
	}
	return out
}
