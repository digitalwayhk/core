package types

import (
	"context"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// 🔧 全局 WebSocket 通知系统（所有 RouterInfo 共享）
type WebSocketNotificationSystem struct {
	jobChan   chan *noticeJob
	workers   int
	closeCh   chan struct{}
	wg        sync.WaitGroup
	once      sync.Once
	isStarted atomic.Bool // 🆕 使用原子操作
	mu        sync.Mutex

	// 🆕 统计信息
	totalJobs     atomic.Int64
	droppedJobs   atomic.Int64
	processedJobs atomic.Int64
}

var (
	globalNotificationSystem *WebSocketNotificationSystem
	globalSystemOnce         sync.Once // 🆕 确保只创建一次
)

// 🆕 获取全局通知系统（单例）
func getGlobalNotificationSystem() *WebSocketNotificationSystem {
	globalSystemOnce.Do(func() {
		globalNotificationSystem = &WebSocketNotificationSystem{
			jobChan: make(chan *noticeJob, 10000), // 🔧 减少缓冲区（10K 足够）
			workers: 20,                           // 🔧 减少 worker 数量
			closeCh: make(chan struct{}),
		}
	})
	return globalNotificationSystem
}

// 🔧 启动全局通知系统（只启动一次）
func (wns *WebSocketNotificationSystem) Start() {
	// 🆕 快速检查
	if wns.isStarted.Load() {
		return
	}

	wns.once.Do(func() {
		// 🆕 双重检查
		if wns.isStarted.Load() {
			return
		}

		logx.Infof("🚀 启动全局 WebSocket 通知系统 (%d workers, 缓冲:%d)",
			wns.workers, cap(wns.jobChan))

		for i := 0; i < wns.workers; i++ {
			wns.wg.Add(1)
			go wns.worker(i)
		}

		wns.isStarted.Store(true)
		logx.Info("✅ 全局 WebSocket 通知系统启动完成")
	})
}

// 🔧 worker 协程（优化性能）
func (wns *WebSocketNotificationSystem) worker(workerID int) {
	defer wns.wg.Done()

	logx.Infof("Worker %d 已启动", workerID)

	for {
		select {
		case job, ok := <-wns.jobChan:
			if !ok {
				// 通道已关闭
				logx.Infof("Worker %d: 通道已关闭", workerID)
				return
			}
			wns.processJob(workerID, job)

		case <-wns.closeCh:
			// 🔧 清空剩余任务
			remaining := 0
			for {
				select {
				case job := <-wns.jobChan:
					wns.processJob(workerID, job)
					remaining++
				default:
					if remaining > 0 {
						logx.Infof("Worker %d 处理了 %d 个剩余任务", workerID, remaining)
					}
					logx.Infof("Worker %d 已停止", workerID)
					return
				}
			}
		}
	}
}

// 🔧 处理任务（添加统计）
func (wns *WebSocketNotificationSystem) processJob(workerID int, job *noticeJob) {
	defer func() {
		if err := recover(); err != nil {
			logx.Errorf("Worker %d panic: %v\nStack: %s",
				workerID, err, debug.Stack())
		}
		// 🆕 无论成功失败都计数
		wns.processedJobs.Add(1)
	}()

	// 🆕 检查 job 的有效性
	if job == nil {
		logx.Errorf("Worker %d: job is nil", workerID)
		return
	}

	if job.router == nil {
		logx.Errorf("Worker %d: job.router is nil for hash:%d", workerID, job.hash)
		return
	}

	if job.iwsr == nil {
		logx.Errorf("Worker %d: job.iwsr is nil for hash:%d", workerID, job.hash)
		return
	}

	// 🔧 带超时的过滤（减少超时时间）
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
				data := job.message
				logx.Errorf("Worker %d NoticeFiltersRouter panic: %v, path: %s, data: %v\nStack: %s",
					workerID, err, path, data, debug.Stack())
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
		logx.Errorf("Worker %d: 过滤超时 hash:%d (1s)", workerID, job.hash)
	}
}

// 🔧 提交任务（非阻塞，带统计）
func (wns *WebSocketNotificationSystem) Submit(job *noticeJob) bool {
	if !wns.isStarted.Load() {
		logx.Errorf("通知系统未启动，丢弃任务")
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

		// 🆕 每 100 个丢弃任务才打印一次
		dropped := wns.droppedJobs.Load()
		if dropped%100 == 0 {
			logx.Errorf("⚠️ 通知队列已满，已丢弃 %d 个任务", dropped)
		}
		return false
	}
}

// 🔧 优雅关闭
func (wns *WebSocketNotificationSystem) Shutdown() {
	if !wns.isStarted.Load() {
		return
	}

	wns.mu.Lock()
	defer wns.mu.Unlock()

	// 再次检查
	if !wns.isStarted.Load() {
		return
	}

	logx.Info("🛑 关闭全局 WebSocket 通知系统...")

	// 🆕 先标记为未启动，拒绝新任务
	wns.isStarted.Store(false)

	// 关闭信号
	close(wns.closeCh)

	// 等待所有 worker 完成（带超时）
	done := make(chan struct{})
	go func() {
		wns.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logx.Info("✅ 所有 worker 已停止")
	case <-time.After(5 * time.Second):
		logx.Error("⚠️ 等待 worker 停止超时（5秒）")
	}

	// 关闭任务通道
	close(wns.jobChan)

	// 🆕 打印最终统计
	logx.Infof("📊 通知系统统计: 总任务:%d, 已处理:%d, 已丢弃:%d",
		wns.totalJobs.Load(),
		wns.processedJobs.Load(),
		wns.droppedJobs.Load())
}

// 🔧 获取统计信息
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

		// 🆕 新增统计
		"total_jobs":     wns.totalJobs.Load(),
		"processed_jobs": wns.processedJobs.Load(),
		"dropped_jobs":   wns.droppedJobs.Load(),
		"success_rate":   float64(wns.processedJobs.Load()) / float64(wns.totalJobs.Load()) * 100,
	}
}

// 🆕 重置统计（用于监控）
func (wns *WebSocketNotificationSystem) ResetStats() {
	wns.totalJobs.Store(0)
	wns.processedJobs.Store(0)
	wns.droppedJobs.Store(0)
}

// 🆕 健康检查
func (wns *WebSocketNotificationSystem) IsHealthy() bool {
	if !wns.isStarted.Load() {
		return false
	}

	// 🔧 检查队列是否接近满
	pending := len(wns.jobChan)
	capacity := cap(wns.jobChan)

	if float64(pending)/float64(capacity) > 0.9 {
		logx.Errorf("⚠️ 通知队列使用率超过 90%")
		return false
	}

	// 🔧 检查丢弃率
	total := wns.totalJobs.Load()
	dropped := wns.droppedJobs.Load()

	if total > 0 && float64(dropped)/float64(total) > 0.1 {
		logx.Errorf("⚠️ 任务丢弃率超过 10%% (%d/%d)", dropped, total)
		return false
	}

	return true
}
