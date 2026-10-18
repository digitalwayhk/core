// Package orderservice 组装 07 订单水平扩展服务的路由、事件和生命周期能力。
package orderservice

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	manageapi "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/manage"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/manage/analysis"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/manage/bizstats"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/manage/reports"
	publicapi "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/public"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/business"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	// 注册 order.* StatSpec
	_ "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/stats"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/transaction"
	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
	"github.com/digitalwayhk/core/pkg/server/observability"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/zeromicro/go-zero/core/logx"
)

// Service 是可水平扩展的订单权威服务。
type Service struct {
	syncEnabled atomic.Bool
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
	routers = append(routers, &bizstats.Query{})
	// 标准经营分析看板：POST /api/manage/shop-order/analysis
	routers = append(routers, &analysis.Dashboard{})
	// 服务级报表目录与视图（子菜单 /report/shop-order/*）
	routers = append(routers, &reports.List{}, &reports.View{})
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

// Start 启用订单服务标准 Outbox 发布能力。
func (s *Service) Start() {
	// ServiceContext 提供当前副本已领取的机房和机器身份，是本地 pending 目录隔离的唯一来源。
	sc := router.GetContext(contract.OrderServiceName)
	if sc == nil {
		panic(fmt.Errorf("订单服务缺失 ServiceContext: %s", contract.OrderServiceName))
	}
	// 远程 MySQL 是订单最终权威库；启动前建表失败时必须 fail fast，不允许进入半可用状态。
	if err := models.EnsureStorage(); err != nil {
		panic(err)
	}

	// 创建阶段只打开当前副本的本地可靠 store，尚不向路由暴露，也不启动远程同步。
	store, err := s.newOrderWriteStore(sc)
	if err != nil {
		panic(err)
	}
	// 只有 target、runtime 和 ServiceContext 资源托管全部成功后，API/business 才能使用该 store。
	if err := s.bindOrderWriteStore(sc, store); err != nil {
		panic(err)
	}
	// Outbox 依赖已受托管的 store，因此必须最后启动。
	if err := s.startOrderInfrastructure(sc); err != nil {
		panic(err)
	}
}

// newOrderWriteStore 为当前订单服务副本创建尚未绑定远程 target 的本地可靠 store。
func (s *Service) newOrderWriteStore(sc *router.ServiceContext) (*transaction.OrderWriteStore, error) {
	// BasePath 只是挂载根目录；ReliableWriteStore 还会追加 service/dc/machine，防止水平副本共用 Badger 文件。
	basePath := orderPendingBasePath()
	badgerConfig := nosql.DefaultProductionConfig(basePath)
	// 示例不转发 Badger 内部噪声日志；存储和同步结果由框架结构化指标表达。
	badgerConfig.EnableLogger = false
	// 统一使用框架自适应 worker：低流量按最长收集时间刷新，积压有进展时连续处理。
	badgerConfig.AutoSync = true
	badgerConfig.SyncBatchSize = 512
	badgerConfig.SyncFlushThreshold = 32
	badgerConfig.SyncMaxCollectDelay = 20 * time.Millisecond
	badgerConfig.SyncBacklogDrainDelay = 0

	return transaction.NewOrderWriteStore(
		// ServiceIdentity 使本地目录与已领取的副本身份绑定；MachineID 变化时不会隐式接管旧 pending。
		nosql.ServiceIdentity{
			ServiceName:  sc.Service.Name,
			DataCenterID: int64(sc.Config.DataCenterID),
			MachineID:    int64(sc.Config.MachineID),
		},
		nosql.ReliableWriteStoreConfig{
			BasePath: basePath,
			Badger:   badgerConfig,
			// 已有一个请求排队时启动 1ms 合批；低积压仍立即提交，避免默认 16 条门槛退化为逐笔落盘。
			Batch: nosql.BatchCommitConfig{
				MaxBatch:       128,
				CollectBacklog: 1,
				CollectWindow:  time.Millisecond,
				QueueCapacity:  1024,
			},
			// 准入控制保护单副本的 fsync、pending 积压和磁盘容量，达到硬边界时 fail closed。
			Admission: nosql.WriteAdmissionConfig{
				MaxConcurrent:      500,
				AcquireTimeout:     2 * time.Second,
				SoftPending:        10_000,
				HardPending:        50_000,
				MaxBacklogDuration: 30 * time.Second,
				HardDiskBytes:      1 << 30, // 当前副本 Badger LSM + VLog 的 1 GiB 硬上限。
			},
			// 关闭只等待已接收的本地批次，不在此超时窗口内访问 MySQL。
			CloseTimeout: 10 * time.Second,
		},
	)
}

// bindOrderWriteStore 先绑定 runtime 和托管资源，再启动 target，失败时解绑并关闭 store。
func (s *Service) bindOrderWriteStore(sc *router.ServiceContext, store *transaction.OrderWriteStore) error {
	runtime := s.ensureRuntime()
	// runtime 是预先注入路由和 business 的稳定引用；Bind 拒绝静默替换已存在的 store。
	if err := runtime.Bind(store); err != nil {
		_ = store.Close(context.Background())
		return err
	}
	// 只有 UseResource 成功后，store 才拥有明确的关闭 owner，Service.Stop 不再自己重复 Close。
	if err := sc.UseResource("order-write-store", store); err != nil {
		// 先断开业务引用，再关闭本地 store，避免并发请求拿到已关闭资源。
		runtime.Unbind()
		_ = store.Close(context.Background())
		return err
	}
	// UseWriteBehind 会立即启动 worker，因此必须晚于 runtime 与资源托管成功。
	s.syncEnabled.Store(true)
	if err := store.UseWriteBehind(business.OrderWriteBehindTarget{
		BeforeSync: func() error {
			if !s.syncEnabled.Load() {
				return transaction.ErrOrderWriteStoreUnavailable
			}
			return sc.CheckServiceAdmission()
		},
		AfterSync: sc.NotifyOutbox,
	}); err != nil {
		s.syncEnabled.Store(false)
		runtime.Unbind()
		_ = store.Close(context.Background())
		return err
	}
	// Pending 组件指标：本进程 Collector → Prometheus；Admin 只查询 Aggregator。
	if err := sc.RegisterRuntimeMetricProviders(observability.ReliableWriteProvider{
		Snapshot: func() observability.ReliableWriteMetricsSnapshot {
			m := store.Metrics()
			return observability.ReliableWriteMetricsSnapshot{
				Pending:   m.Pending,
				DiskBytes: m.BadgerLSMBytes + m.BadgerVLogBytes,
				SyncFail:  float64(m.Sync.Failures),
			}
		},
	}, writePathProvider{snapshot: store.Metrics}); err != nil {
		logx.Errorw("runtime_metric_provider_register_failed",
			logx.Field("service", sc.Service.Name),
			logx.Field("component", "pending"),
			logx.Field("error", err),
		)
	}
	return nil
}

// startOrderInfrastructure 在可靠 store 已绑定且已受生命周期托管后，启动远程汇合配套设施。
func (s *Service) startOrderInfrastructure(sc *router.ServiceContext) error {
	// MySQL Outbox 与订单 upsert 同事务写入，由标准 ServiceEventBridge 发布，业务不自建 worker。
	if err := sc.UseOutboxWithOptions(models.OutboxStore{}, router.OutboxRuntimeOptions{KeyConcurrency: 4}); err != nil {
		return err
	}
	// 业务统计：定时聚合 Order → StatsStore，API 只读快照。
	business.SharedStatsRunner.Start()
	return nil
}

// Stop 阻止新的后台业务批次并解绑入口；worker 和 store 由 ServiceContext 关闭。
func (s *Service) Stop() {
	s.syncEnabled.Store(false)
	business.SharedStatsRunner.Stop()
	s.ensureRuntime().Unbind()
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
