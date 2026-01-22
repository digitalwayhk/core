package types

import (
	"context"
	"runtime/debug"
	"sync"
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
	isStarted bool
	mu        sync.Mutex
}

var globalNotificationSystem = &WebSocketNotificationSystem{
	jobChan: make(chan *noticeJob, 50000), // 增大缓冲
	workers: 50,                           // 增加 worker 数量
	closeCh: make(chan struct{}),
}

// 🔧 启动全局通知系统（只启动一次）
func (wns *WebSocketNotificationSystem) Start() {
	wns.once.Do(func() {
		wns.mu.Lock()
		defer wns.mu.Unlock()

		if wns.isStarted {
			return
		}

		logx.Infof("🚀 启动全局 WebSocket 通知系统 (%d workers, 缓冲:%d)",
			wns.workers, cap(wns.jobChan))

		for i := 0; i < wns.workers; i++ {
			wns.wg.Add(1)
			go wns.worker(i)
		}

		wns.isStarted = true
	})
}

// 🔧 worker 协程
func (wns *WebSocketNotificationSystem) worker(workerID int) {
	defer wns.wg.Done()

	for {
		select {
		case job := <-wns.jobChan:
			wns.processJob(workerID, job)
		case <-wns.closeCh:
			// 清空剩余任务
			for {
				select {
				case job := <-wns.jobChan:
					wns.processJob(workerID, job)
				default:
					logx.Infof("Worker %d 已停止", workerID)
					return
				}
			}
		}
	}
}

// 🔧 处理任务（添加更多检查）
func (wns *WebSocketNotificationSystem) processJob(workerID int, job *noticeJob) {
	defer func() {
		if err := recover(); err != nil {
			logx.Errorf("Worker %d panic: %v\nStack: %s",
				workerID, err, debug.Stack())
		}
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

	// 🔧 带超时的过滤
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct {
		ok   bool
		data interface{}
	}, 1)

	go func() {
		defer func() {
			if err := recover(); err != nil {
				logx.Errorf("NoticeFiltersRouter panic: %v", err)
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
		if result.ok {
			// 🆕 在调用前再次检查
			if job.router != nil {
				job.router.sendToHashClients(job.hash, job.message, result.data)
			}
		}
	case <-ctx.Done():
		logx.Errorf("Worker %d: 过滤超时 hash:%d", workerID, job.hash)
	}
}

// 🔧 提交任务（非阻塞）
func (wns *WebSocketNotificationSystem) Submit(job *noticeJob) bool {
	select {
	case wns.jobChan <- job:
		return true
	default:
		// 队列满，记录并丢弃
		logx.Errorf("⚠️ 通知队列已满 (缓冲:%d), 丢弃任务 hash:%d",
			cap(wns.jobChan), job.hash)
		return false
	}
}

// 🔧 优雅关闭
func (wns *WebSocketNotificationSystem) Shutdown() {
	wns.mu.Lock()
	defer wns.mu.Unlock()

	if !wns.isStarted {
		return
	}

	logx.Info("🛑 关闭全局 WebSocket 通知系统...")
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
	case <-time.After(10 * time.Second):
		logx.Error("⚠️ 等待 worker 停止超时")
	}

	close(wns.jobChan)
	wns.isStarted = false
}

// 🔧 获取统计信息
func (wns *WebSocketNotificationSystem) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"workers":        wns.workers,
		"pending_jobs":   len(wns.jobChan),
		"queue_capacity": cap(wns.jobChan),
		"queue_usage":    float64(len(wns.jobChan)) / float64(cap(wns.jobChan)) * 100,
		"is_queue_full":  len(wns.jobChan) >= cap(wns.jobChan)-100,
		"is_started":     wns.isStarted,
	}
}
