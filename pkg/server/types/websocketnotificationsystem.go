package types

import (
	"context"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/proc"
)

// 全局 WebSocket 通知系统（所有 RouterInfo 共享）
type WebSocketNotificationSystem struct {
	jobChan   chan *noticeJob
	workers   int
	closeCh   chan struct{}
	statsDone chan struct{}
	wg        sync.WaitGroup
	once      sync.Once
	isStarted atomic.Bool //  使用原子操作
	isStopped atomic.Bool
	mu        sync.RWMutex

	//  统计信息
	totalJobs     atomic.Int64
	droppedJobs   atomic.Int64
	processedJobs atomic.Int64
}

var (
	globalNotificationSystem *WebSocketNotificationSystem
	globalSystemOnce         sync.Once //  确保只创建一次
	globalSystemMu           sync.RWMutex
	websocketShutdownOnce    sync.Once
)

// 获取全局通知系统（单例）
func getGlobalNotificationSystem() *WebSocketNotificationSystem {
	globalSystemOnce.Do(func() {
		system := &WebSocketNotificationSystem{
			jobChan: make(chan *noticeJob, 10000), //  减少缓冲区（10K 足够）
			workers: 20,                           //  减少 worker 数量
			closeCh: make(chan struct{}),
		}
		globalSystemMu.Lock()
		globalNotificationSystem = system
		globalSystemMu.Unlock()
		registerWebSocketProcessShutdown()
	})
	globalSystemMu.RLock()
	system := globalNotificationSystem
	globalSystemMu.RUnlock()
	return system
}

func registerWebSocketProcessShutdown() {
	websocketShutdownOnce.Do(func() {
		proc.AddShutdownListener(func() {
			globalSystemMu.RLock()
			system := globalNotificationSystem
			globalSystemMu.RUnlock()
			if system != nil {
				system.Shutdown()
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := StopPeriodicCleanup(ctx); err != nil {
				logx.Errorw("websocket_cleanup_stop_failed", logx.Field("error", err))
			}
		})
	})
}

// 启动全局通知系统（只启动一次）
func (wns *WebSocketNotificationSystem) Start() {
	wns.mu.Lock()
	defer wns.mu.Unlock()
	if wns.isStarted.Load() || wns.isStopped.Load() {
		return
	}
	wns.once.Do(func() {
		logx.Infow("websocket_notification_started",
			logx.Field("workers", wns.workers),
			logx.Field("queue_capacity", cap(wns.jobChan)),
		)

		for i := 0; i < wns.workers; i++ {
			wns.wg.Add(1)
			go wns.worker(i)
		}

		wns.isStarted.Store(true)

		//  每 5 分钟重置统计，防止历史 droppedJobs 累积导致 IsHealthy 永久返回 false
		wns.wg.Add(1)
		wns.statsDone = make(chan struct{})
		go func() {
			defer wns.wg.Done()
			defer close(wns.statsDone)
			ticker := time.NewTicker(5 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					wns.ResetStats()
				case <-wns.closeCh:
					return
				}
			}
		}()
	})
}

// worker 协程（优化性能）
func (wns *WebSocketNotificationSystem) worker(workerID int) {
	defer wns.wg.Done()

	logx.Debugw("websocket_worker_started", logx.Field("worker_id", workerID))

	for {
		select {
		case job, ok := <-wns.jobChan:
			if !ok {
				// 通道已关闭
				logx.Debugw("websocket_worker_stopped",
					logx.Field("worker_id", workerID),
					logx.Field("reason", "queue_closed"),
				)
				return
			}
			wns.processJob(workerID, job)

		case <-wns.closeCh:
			//  清空剩余任务
			remaining := 0
			for {
				select {
				case job := <-wns.jobChan:
					wns.processJob(workerID, job)
					remaining++
				default:
					logx.Debugw("websocket_worker_stopped",
						logx.Field("worker_id", workerID),
						logx.Field("drained_jobs", remaining),
						logx.Field("reason", "shutdown"),
					)
					return
				}
			}
		}
	}
}

// 处理任务（添加统计）
func (wns *WebSocketNotificationSystem) processJob(workerID int, job *noticeJob) {
	defer func() {
		if err := recover(); err != nil {
			logx.Errorw("websocket_worker_panicked",
				logx.Field("worker_id", workerID),
				logx.Field("error", err),
				logx.Field("stack", string(debug.Stack())),
			)
		}
		//  无论成功失败都计数
		wns.processedJobs.Add(1)
	}()

	//  检查 job 的有效性
	if job == nil {
		logx.Errorw("websocket_job_invalid",
			logx.Field("worker_id", workerID),
			logx.Field("reason", "job_nil"),
		)
		return
	}

	if job.router == nil {
		logx.Errorw("websocket_job_invalid",
			logx.Field("worker_id", workerID),
			logx.Field("hash", job.hash),
			logx.Field("reason", "router_nil"),
		)
		return
	}

	if job.iwsr == nil {
		logx.Errorw("websocket_job_invalid",
			logx.Field("worker_id", workerID),
			logx.Field("hash", job.hash),
			logx.Field("reason", "notice_router_nil"),
		)
		return
	}

	//  带超时的过滤（减少超时时间）
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	done := make(chan struct {
		ok   bool
		data interface{}
	}, 1)

	go func() {
		defer func() {
			if err := recover(); err != nil {
				path := job.router.GetPath()
				logx.Errorw("websocket_filter_panicked",
					logx.Field("worker_id", workerID),
					logx.Field("route", path),
					logx.Field("error", err),
					logx.Field("stack", string(debug.Stack())),
				)
				done <- struct {
					ok   bool
					data interface{}
				}{false, nil}
			}
		}()

		ok, ndata := job.iwsr.NoticeFiltersRouter(job.message, job.api)
		done <- struct {
			ok   bool
			data interface{}
		}{ok, ndata}
	}()

	select {
	case result := <-done:
		if result.ok && job.router != nil {
			job.router.sendToHashClients(job.hash, job.message, result.data)
		}
	case <-ctx.Done():
		logx.Errorw("websocket_filter_timeout",
			logx.Field("worker_id", workerID),
			logx.Field("hash", job.hash),
			logx.Field("timeout_ms", 1000),
		)
	}
}

// 提交任务（非阻塞，带统计）
func (wns *WebSocketNotificationSystem) Submit(job *noticeJob) bool {
	wns.mu.RLock()
	defer wns.mu.RUnlock()
	if !wns.isStarted.Load() {
		logx.Errorw("websocket_job_dropped", logx.Field("reason", "system_not_started"))
		wns.droppedJobs.Add(1)
		return false
	}

	wns.totalJobs.Add(1)

	select {
	case wns.jobChan <- job:
		return true
	default:
		// 队列满，记录并丢弃
		wns.droppedJobs.Add(1)

		//  每 100 个丢弃任务才打印一次
		dropped := wns.droppedJobs.Load()
		if dropped%100 == 0 {
			logx.Errorw("websocket_job_dropped",
				logx.Field("reason", "queue_full"),
				logx.Field("dropped_total", dropped),
			)
		}
		return false
	}
}

// 优雅关闭
func (wns *WebSocketNotificationSystem) Shutdown() {
	wns.mu.Lock()
	if wns.isStopped.Load() {
		wns.mu.Unlock()
		return
	}
	wns.isStopped.Store(true)
	if !wns.isStarted.Load() {
		wns.mu.Unlock()
		return
	}

	logx.Infow("websocket_notification_stopping")

	//  先标记为未启动，拒绝新任务
	wns.isStarted.Store(false)

	// 关闭信号
	close(wns.closeCh)
	wns.mu.Unlock()

	// 等待所有 worker 完成（带超时）
	done := make(chan struct{})
	go func() {
		wns.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logx.Infow("websocket_notification_stopped",
			logx.Field("total_jobs", wns.totalJobs.Load()),
			logx.Field("processed_jobs", wns.processedJobs.Load()),
			logx.Field("dropped_jobs", wns.droppedJobs.Load()),
		)
	case <-time.After(5 * time.Second):
		logx.Errorw("websocket_shutdown_timeout", logx.Field("timeout_ms", 5000))
	}
}

// 获取统计信息
func (wns *WebSocketNotificationSystem) GetStats() map[string]interface{} {
	pending := len(wns.jobChan)
	capacity := cap(wns.jobChan)

	return map[string]interface{}{
		"workers":         wns.workers,
		"pending_jobs":    pending,
		"queue_capacity":  capacity,
		"queue_usage_pct": float64(pending) / float64(capacity) * 100,
		"is_queue_full":   pending >= capacity-100,
		"is_started":      wns.isStarted.Load(),

		//  新增统计
		"total_jobs":     wns.totalJobs.Load(),
		"processed_jobs": wns.processedJobs.Load(),
		"dropped_jobs":   wns.droppedJobs.Load(),
		"success_rate":   float64(wns.processedJobs.Load()) / float64(wns.totalJobs.Load()) * 100,
	}
}

// 重置统计（用于监控）
func (wns *WebSocketNotificationSystem) ResetStats() {
	wns.totalJobs.Store(0)
	wns.processedJobs.Store(0)
	wns.droppedJobs.Store(0)
}

// 健康检查
func (wns *WebSocketNotificationSystem) IsHealthy() bool {
	if !wns.isStarted.Load() {
		return false
	}

	//  检查队列是否接近满
	pending := len(wns.jobChan)
	capacity := cap(wns.jobChan)

	if float64(pending)/float64(capacity) > 0.9 {
		logx.Errorw("websocket_notification_unhealthy",
			logx.Field("reason", "queue_usage"),
			logx.Field("pending_jobs", pending),
			logx.Field("queue_capacity", capacity),
		)
		return false
	}

	//  检查丢弃率
	total := wns.totalJobs.Load()
	dropped := wns.droppedJobs.Load()

	if total > 0 && float64(dropped)/float64(total) > 0.1 {
		logx.Errorw("websocket_notification_unhealthy",
			logx.Field("reason", "drop_rate"),
			logx.Field("dropped_jobs", dropped),
			logx.Field("total_jobs", total),
		)
		return false
	}

	return true
}
