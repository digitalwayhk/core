// Package orderservice 组装 07 订单水平扩展服务的路由、事件和生命周期能力。
package orderservice

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	manageapi "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/manage"
	publicapi "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/public"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/business"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/transaction"
	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/zeromicro/go-zero/core/logx"
)

// Service 是可水平扩展的订单权威服务。
type Service struct {
	mu          sync.Mutex
	cancelSync  context.CancelFunc
	syncDone    chan struct{}
	runtimeOnce sync.Once
	runtime     *transaction.OrderWriteRuntime
}

// ServiceName 返回订单服务稳定逻辑名，多个副本共享该名称。
func (*Service) ServiceName() string { return contract.OrderServiceName }

// Routers 注册订单内部 Public API 和管理员 Manage API。
func (s *Service) Routers() []servertypes.IRouter {
	runtime := s.ensureRuntime()
	routers := []servertypes.IRouter{
		publicapi.NewCreateOrder(runtime),
		publicapi.NewCancelOrder(runtime),
		publicapi.NewCreatePayment(runtime),
		publicapi.NewGetOrders(runtime),
		&publicapi.GetPaymentTypes{},
	}
	routers = append(routers, manageapi.NewOrderRuleManage().Routers()...)
	routers = append(routers, manageapi.NewPaymentTypeManage().Routers()...)
	routers = append(routers, manageapi.NewOrderManage().Routers()...)
	return routers
}

// OnAuthRequest 将 Order Manage 限制为平台管理员；内部 Public 由 WithInternalCallers 校验。
func (*Service) OnAuthRequest(ctx context.Context, args servertypes.AuthRequestArgs) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if args.PathType == servertypes.ManageType && args.Identity.UID != contract.PlatformAdminUserID {
		return servertypes.NewPublicError(servertypes.ErrorKindForbidden, servertypes.PublicCodeForbidden, "权限不足", contract.ErrForbidden)
	}
	return nil
}

// SubscribeRouters 保留旧观察路由兼容入口；事件订阅统一使用 ServiceEventBridge。
func (*Service) SubscribeRouters() []*servertypes.ObserveArgs { return nil }

// Start 启用订单服务标准 Outbox 发布能力。
func (s *Service) Start() {
	sc := router.GetContext(contract.OrderServiceName)
	if sc == nil {
		panic(fmt.Errorf("订单服务缺失 ServiceContext: %s", contract.OrderServiceName))
	}
	if err := models.EnsureStorage(); err != nil {
		panic(err)
	}
	basePath := orderPendingBasePath()
	badgerConfig := nosql.DefaultProductionConfig(basePath)
	badgerConfig.EnableLogger = false
	badgerConfig.AutoSync = false
	store, err := transaction.NewOrderWriteStore(
		nosql.ServiceIdentity{
			ServiceName:  sc.Service.Name,
			DataCenterID: int64(sc.Config.DataCenterID),
			MachineID:    int64(sc.Config.MachineID),
		},
		nosql.ReliableWriteStoreConfig{
			BasePath: basePath,
			Badger:   badgerConfig,
			Batch: nosql.BatchCommitConfig{
				MaxBatch:      128,
				CollectWindow: time.Millisecond,
				QueueCapacity: 1024,
			},
			Admission: nosql.WriteAdmissionConfig{
				MaxConcurrent:      500,
				AcquireTimeout:     2 * time.Second,
				SoftPending:        10_000,
				HardPending:        50_000,
				MaxBacklogDuration: 30 * time.Second,
				HardDiskBytes:      1 << 30,
			},
			CloseTimeout: 10 * time.Second,
		},
	)
	if err != nil {
		panic(err)
	}
	if err := store.UseWriteBehind(business.OrderWriteBehindTarget{}); err != nil {
		_ = store.Close(context.Background())
		panic(err)
	}
	if err := s.ensureRuntime().Bind(store); err != nil {
		_ = store.Close(context.Background())
		panic(err)
	}
	if err := sc.UseResource("order-write-store", store); err != nil {
		s.ensureRuntime().Unbind()
		_ = store.Close(context.Background())
		panic(err)
	}
	if err := sc.UseOutbox(models.OutboxStore{}); err != nil {
		panic(err)
	}
	s.startPendingSync(sc, s.ensureRuntime())
}

// Stop 停止订单本地 pending 同步循环。
func (s *Service) Stop() {
	s.mu.Lock()
	cancel := s.cancelSync
	done := s.syncDone
	s.cancelSync = nil
	s.syncDone = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
	s.ensureRuntime().Unbind()
}

// startPendingSync 启动本实例本地 pending 到共享远程权威库的后台同步循环。
func (s *Service) startPendingSync(sc *router.ServiceContext, store business.OrderSyncStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancelSync != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.cancelSync = cancel
	s.syncDone = done
	go runPendingSyncLoop(ctx, sc, store, done)
}

// runPendingSyncLoop 周期性同步当前副本的本地 pending，并唤醒标准 Outbox 发布器。
func runPendingSyncLoop(ctx context.Context, sc *router.ServiceContext, store business.OrderSyncStore, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	syncer := business.RemoteOrderSyncer{Store: store}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			result, err := syncer.DrainOnce(ctx, 100)
			if err != nil {
				logx.Errorw("shop_order_pending_sync_failed", logx.Field("service", contract.OrderServiceName), logx.Field("error", err))
				continue
			}
			if result.Confirmed > 0 {
				sc.NotifyOutbox()
			}
		}
	}
}

func (s *Service) ensureRuntime() *transaction.OrderWriteRuntime {
	s.runtimeOnce.Do(func() { s.runtime = transaction.NewOrderWriteRuntime() })
	return s.runtime
}

func orderPendingBasePath() string {
	if path := os.Getenv("SHOP_LOCAL_PENDING_DIR"); path != "" {
		return path
	}
	return filepath.Join(utils.Getpath(), "data", "order-scale-pending")
}
