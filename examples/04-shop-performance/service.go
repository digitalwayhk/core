package performanceshop

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/digitalwayhk/core/examples/04-shop-performance/api/manage"
	privateapi "github.com/digitalwayhk/core/examples/04-shop-performance/api/private"
	publicapi "github.com/digitalwayhk/core/examples/04-shop-performance/api/public"
	"github.com/digitalwayhk/core/examples/04-shop-performance/business"
	"github.com/digitalwayhk/core/examples/04-shop-performance/contract"
	"github.com/digitalwayhk/core/examples/04-shop-performance/models"
	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/zeromicro/go-zero/core/logx"
)

// ShopService 组装模型、实例级订单 runtime 与 Manage 继承能力。
type ShopService struct {
	runtimeOnce sync.Once
	runtime     *models.OrderWriteRuntime
}

// ServiceName 返回继承商城的稳定服务名。
func (own *ShopService) ServiceName() string { return contract.ServiceName }

// Routers 返回继承商城的 Manage、Public 和 Private 路由。
func (own *ShopService) Routers() []types.IRouter {
	runtime := own.orderRuntime()
	orders := business.NewOrderService(runtime)
	payments := business.NewPaymentService(runtime)
	routers := make([]types.IRouter, 0, 36)
	routers = append(routers, manage.NewProductManage(runtime).Routers()...)
	routers = append(routers, manage.NewSupplierManage().Routers()...)
	routers = append(routers, manage.NewPaymentTypeManage().Routers()...)
	routers = append(routers, manage.NewOrderManage().Routers()...)
	routers = append(routers, manage.NewPaymentRecordManage(runtime).Routers()...)
	routers = append(routers,
		&publicapi.GetProducts{},
		&publicapi.GetSuppliers{},
		&publicapi.GetPaymentTypes{},
		privateapi.NewAddOrder(orders),
		privateapi.NewGetOrders(orders),
		privateapi.NewDeleteOrder(orders),
		privateapi.NewCreatePayment(payments),
		privateapi.NewCancelOrder(orders),
	)
	return routers
}

// SubscribeRouters 返回内部服务观察订阅；本示例没有跨服务订阅。
func (own *ShopService) SubscribeRouters() []*types.ObserveArgs { return nil }

// Start 启动两个 ServiceContext 级性能组件：
//   - OrderWriteStore：通过可靠 Group Commit 写入 Badger，再异步汇合 SQLite。
//   - OrderReferenceCache：缓存下单所需的商品/供应商事实，失效回调统一经过 EventBridge。
func (own *ShopService) Start() {
	serviceContext := router.GetContext(contract.ServiceName)
	if serviceContext == nil {
		logx.Errorw("shop_order_write_store_start_failed", logx.Field("error", "service context unavailable"))
		return
	}
	if err := models.EnsureStorage(); err != nil {
		logx.Errorw("shop_order_write_store_start_failed", logx.Field("error", err))
		return
	}
	basePath := filepath.Join(utils.Getpath(), "data", "order-write-behind")
	badgerConfig := nosql.DefaultProductionConfig(basePath)
	badgerConfig.EnableLogger = false
	badgerConfig.AutoSync = true
	badgerConfig.SyncBatchDelay = 500 * time.Millisecond
	store, err := models.NewOrderWriteStore(
		nosql.ServiceIdentity{
			ServiceName:  serviceContext.Service.Name,
			DataCenterID: int64(serviceContext.Config.DataCenterID),
			MachineID:    int64(serviceContext.Config.MachineID),
		},
		models.CloneDataAction(),
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
		logx.Errorw("shop_order_write_store_start_failed", logx.Field("error", err))
		return
	}
	if err := own.orderRuntime().Bind(store); err != nil {
		_ = store.Close(nil)
		logx.Errorw("shop_order_write_store_bind_failed", logx.Field("error", err))
		return
	}
	if err := serviceContext.UseResource("order-write-store", store); err != nil {
		own.orderRuntime().Unbind()
		_ = store.Close(nil)
		logx.Errorw("shop_order_write_store_resource_failed", logx.Field("error", err))
		return
	}
	if serviceContext.ServiceEventBridge == nil {
		logx.Errorw("shop_order_reference_cache_start_failed", logx.Field("error", "service event bridge unavailable"))
		return
	}
	if err := business.StartOrderReferenceCache(serviceContext.ServiceEventBridge); err != nil {
		logx.Errorw("shop_order_reference_cache_start_failed", logx.Field("error", err))
	}
}

// Stop 先断开请求入口并取消事实缓存；store 由 ServiceContext 随后关闭。
func (own *ShopService) Stop() {
	own.orderRuntime().Unbind()
	business.StopOrderReferenceCache()
}

func (own *ShopService) orderRuntime() *models.OrderWriteRuntime {
	own.runtimeOnce.Do(func() { own.runtime = models.NewOrderWriteRuntime() })
	return own.runtime
}
