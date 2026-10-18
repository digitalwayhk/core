// 本文件管理独立对照服务的原生连接与 goroutine 生命周期。
package reference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
	"gorm.io/gorm"
)

type application struct {
	c          Config
	file       fileConfig
	db         *gorm.DB
	local      *pending
	redis      *redis.Client
	clock      clock
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	clients    map[string]zrpc.Client
	requestIDs sync.Map
	processed  sync.Map
	ruleMu     sync.RWMutex
	rule       *OrderRule
	notify     chan struct{}
	grpcServer *grpc.Server
	serverMu   sync.Mutex
}

// Run 启动一个原生角色；仅使用底层库，不调用 Core 服务、事件或存储封装。
func Run(c Config) error {
	f, e := loadFile(c.CoreJSON)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithCancel(context.Background())
	a := &application{c: c, file: f, ctx: ctx, cancel: cancel, clients: map[string]zrpc.Client{}, notify: make(chan struct{}, 1)}
	a.clock.worker = 1
	if c.Role == "order" {
		a.clock.worker = 2
	}
	defer a.close()
	if c.Role == "order" || c.Role == "supplier" {
		a.db, e = openDatabase(c.Role, c.SupplierDB)
		if e != nil {
			return e
		}
	}
	if c.Role == "order" {
		a.local, e = openPending(c.PendingDir)
		if e != nil {
			return e
		}
	}
	if c.Role == "user" || c.Role == "order" {
		a.redis = redis.NewClient(&redis.Options{Addr: os.Getenv("SHOP_REDIS_ADDR")})
		if e = a.redis.Ping(ctx).Err(); e != nil {
			return e
		}
	}
	if c.Role == "user" {
		for name, addr := range map[string]string{"shop-supplier": c.SupplierRPC, "shop-order": c.OrderRPC} {
			a.clients[name], e = newRPCClient(name, addr)
			if e != nil {
				return e
			}
		}
		if e = a.redis.XGroupCreateMkStream(ctx, streamName, "shop-user", "0").Err(); e != nil && !isBusyGroup(e) {
			return e
		}
		a.wg.Go(a.consume)
	}
	if c.Role == "order" {
		a.wg.Go(a.syncLoop)
		a.wg.Go(a.publishLoop)
	}
	if c.Role != "user" {
		if e = a.startRPC(); e != nil {
			return e
		}
	}
	server, e := a.newHTTP()
	if e != nil {
		return e
	}
	defer server.Stop()
	go server.Start()
	metrics, e := a.startMetrics()
	if e != nil {
		return e
	}
	defer func() {
		shutdown, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		_ = metrics.Shutdown(shutdown)
	}()
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-signalCtx.Done()
	return nil
}

func (a *application) close() {
	a.cancel()
	a.serverMu.Lock()
	server := a.grpcServer
	a.serverMu.Unlock()
	if server != nil {
		server.Stop()
	}
	a.wg.Wait()
	if a.local != nil {
		if e := a.local.close(); e != nil {
			fmt.Fprintln(os.Stderr, e)
		}
	}
	for _, c := range a.clients {
		_ = c.Conn().Close()
	}
	if a.redis != nil {
		_ = a.redis.Close()
	}
	if a.db != nil {
		if p, e := a.db.DB(); e == nil {
			_ = p.Close()
		}
	}
}

func envInt(name string, fallback int) int {
	v, e := strconv.Atoi(os.Getenv(name))
	if e != nil || v <= 0 {
		return fallback
	}
	return v
}
func wait(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (a *application) syncLoop() {
	for a.ctx.Err() == nil {
		if a.local.depth.Load() < 32 {
			timer := time.NewTimer(20 * time.Millisecond)
		collect:
			for a.local.depth.Load() < 32 {
				select {
				case <-a.ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
					break collect
				case <-a.local.wake:
				}
			}
			timer.Stop()
		}
		items, e := a.local.batch(512)
		if e == nil && len(items) > 0 {
			e = a.syncBatch(a.ctx, items)
			if e == nil {
				e = a.local.confirm(items)
			}
			if e == nil {
				select {
				case a.notify <- struct{}{}:
				default:
				}
			}
		}
		if e != nil {
			recordError("sync", e)
			if !wait(a.ctx, 100*time.Millisecond) {
				return
			}
		}
	}
}

func recordError(phase string, e error) {
	if errors.Is(e, context.Canceled) {
		return
	}
	b, _ := json.Marshal(map[string]string{"event": "native_operation_failed", "phase": phase, "error": e.Error()})
	fmt.Fprintln(os.Stderr, string(b))
}
