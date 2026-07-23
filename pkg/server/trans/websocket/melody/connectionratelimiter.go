package melody

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// 新增：连接频率限制器
type ConnectionRateLimiter struct {
	clients   map[string]*rate.Limiter
	mu        sync.RWMutex
	stopCh    chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

func NewConnectionRateLimiter() *ConnectionRateLimiter {
	return newConnectionRateLimiter(5 * time.Minute)
}

func newConnectionRateLimiter(cleanupInterval time.Duration) *ConnectionRateLimiter {
	crl := &ConnectionRateLimiter{
		clients: make(map[string]*rate.Limiter),
		stopCh:  make(chan struct{}),
		done:    make(chan struct{}),
	}

	go func() {
		defer close(crl.done)
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				crl.cleanup()
			case <-crl.stopCh:
				return
			}
		}
	}()

	return crl
}

func (crl *ConnectionRateLimiter) Close() {
	crl.closeOnce.Do(func() { close(crl.stopCh) })
	<-crl.done
}

func (crl *ConnectionRateLimiter) Allow(ip string) bool {
	crl.mu.RLock()
	limiter, exists := crl.clients[ip]
	crl.mu.RUnlock()

	if !exists {
		crl.mu.Lock()
		// 每秒最多5个连接，突发允许10个
		limiter = rate.NewLimiter(5, 10)
		crl.clients[ip] = limiter
		crl.mu.Unlock()
	}

	return limiter.Allow()
}

func (crl *ConnectionRateLimiter) cleanup() {
	crl.mu.Lock()
	defer crl.mu.Unlock()

	// 简单清理策略：清空所有限制器
	// 实际应用中可以基于时间戳进行更精细的清理
	if len(crl.clients) > 1000 {
		crl.clients = make(map[string]*rate.Limiter)
	}
}

type ConnectionCounter struct {
	count int64
	mu    sync.RWMutex
}

func (cc *ConnectionCounter) Increment() int64 {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.count++
	return cc.count
}

func (cc *ConnectionCounter) Decrement() int64 {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	cc.count--
	return cc.count
}

func (cc *ConnectionCounter) Get() int64 {
	cc.mu.RLock()
	defer cc.mu.RUnlock()
	return cc.count
}
