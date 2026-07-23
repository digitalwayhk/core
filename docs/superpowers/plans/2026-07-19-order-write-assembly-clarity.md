# 07 订单可靠写装配可读性 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 07 order-service 的可靠写装配拆成三个职责清晰的 Service 私有方法，并用逐逻辑步骤中文注释解释 Service 与 models 的生命周期和数据语义。

**Architecture:** `Service.Start` 只保留启动编排，`newOrderWriteStore` 负责实例身份与本地可靠 store 配置，`bindOrderWriteStore` 负责 target/runtime/resource 顺序绑定与失败回滚，`startOrderInfrastructure` 负责 Outbox 与 bounded sync。models 继续作为框架 `ReliableWriteStore` 的薄领域适配，不新增能力。

**Tech Stack:** Go、`ReliableWriteStore`、Badger、`ServiceContext`、go-zero `logx`、`testify/require`、Docker Compose UAT。

---

## 文件边界

- Create: `examples/07-shop-order-scale/order-service/service_order_write_test.go`
  - 只测试 Service 层 store 工厂、绑定和失败回滚。
- Modify: `examples/07-shop-order-scale/order-service/service.go`
  - 保留服务编排，新增三个私有装配方法和详细注释。
- Modify: `examples/07-shop-order-scale/order-service/models/transaction/order_write_store.go`
  - 详细解释本地可靠确认、用户 pending 扫描、Admin 隔离和关闭语义。
- Modify: `examples/07-shop-order-scale/order-service/models/transaction/order_write_runtime.go`
  - 详细解释实例级稳定引用、绑定/解绑/关闭分工和读锁边界。
- Modify: `examples/07-shop-order-scale/order-service/models/models.go`
  - 补充两个兼容别名的边界说明，不增加实现。

### Task 1: 用测试锁定 Service store 装配边界

**Files:**
- Create: `examples/07-shop-order-scale/order-service/service_order_write_test.go`
- Test: `examples/07-shop-order-scale/order-service/service_order_write_test.go`

- [ ] **Step 1: 写入工厂与回滚失败测试**

```go
// Package orderservice 验证 07 订单服务本地可靠 store 的实例目录和装配回滚边界。
package orderservice

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/transaction"
	"github.com/digitalwayhk/core/pkg/persistence/database/nosql"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func newOrderWriteAssemblyTestContext() *router.ServiceContext {
	return &router.ServiceContext{
		Service: &servertypes.Service{Name: contract.OrderServiceName},
		Config:  &config.ServerConfig{DataCenterID: 3, MachineID: 9},
	}
}

func TestNewOrderWriteStoreUsesCurrentServiceIdentity(t *testing.T) {
	basePath := t.TempDir()
	t.Setenv("SHOP_LOCAL_PENDING_DIR", basePath)
	sc := newOrderWriteAssemblyTestContext()
	service := &Service{}

	store, err := service.newOrderWriteStore(sc)
	require.NoError(t, err)
	resolvedPath := filepath.Join(basePath, contract.OrderServiceName, "dc-3", "machine-9")
	t.Cleanup(func() {
		_ = store.Close(context.Background())
		_ = nosql.CloseSharedManager(resolvedPath)
	})
	require.DirExists(t, resolvedPath)
}

func TestBindOrderWriteStoreUnbindsWhenResourceRegistrationFails(t *testing.T) {
	basePath := t.TempDir()
	t.Setenv("SHOP_LOCAL_PENDING_DIR", basePath)
	sc := newOrderWriteAssemblyTestContext()
	service := &Service{}
	store, err := service.newOrderWriteStore(sc)
	require.NoError(t, err)
	resolvedPath := filepath.Join(basePath, contract.OrderServiceName, "dc-3", "machine-9")
	t.Cleanup(func() { _ = nosql.CloseSharedManager(resolvedPath) })

	err = service.bindOrderWriteStore(sc, store)
	require.ErrorIs(t, err, router.ErrResourceManagerClosed)
	require.ErrorIs(t, service.ensureRuntime().Save(context.Background(), transaction.NewOrder()), transaction.ErrOrderWriteStoreUnavailable)
}
```

- [ ] **Step 2: 运行测试并确认 RED**

Run:

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/07-shop-order-scale/order-service -run 'TestNewOrderWriteStore|TestBindOrderWriteStore' -count=1
```

Expected: FAIL，编译错误明确指向 `service.newOrderWriteStore` 和 `service.bindOrderWriteStore` 尚未定义。

- [ ] **Step 3: 提交 RED 测试**

```bash
rtk git add examples/07-shop-order-scale/order-service/service_order_write_test.go
rtk git commit -m "test: define order write assembly boundaries"
```

### Task 2: 抽取 Service 装配方法并补齐 models 注释

**Files:**
- Modify: `examples/07-shop-order-scale/order-service/service.go`
- Modify: `examples/07-shop-order-scale/order-service/models/transaction/order_write_store.go`
- Modify: `examples/07-shop-order-scale/order-service/models/transaction/order_write_runtime.go`
- Modify: `examples/07-shop-order-scale/order-service/models/models.go`
- Test: `examples/07-shop-order-scale/order-service/service_order_write_test.go`

- [ ] **Step 1: 将 `Service.Start` 收缩为高层编排**

```go
func (s *Service) Start() {
	sc := router.GetContext(contract.OrderServiceName)
	if sc == nil {
		panic(fmt.Errorf("订单服务缺失 ServiceContext: %s", contract.OrderServiceName))
	}
	if err := models.EnsureStorage(); err != nil {
		panic(err)
	}
	store, err := s.newOrderWriteStore(sc)
	if err != nil {
		panic(err)
	}
	if err := s.bindOrderWriteStore(sc, store); err != nil {
		panic(err)
	}
	if err := s.startOrderInfrastructure(sc); err != nil {
		panic(err)
	}
}
```

在每个逻辑段前添加中文注释，分别解释“启动前置条件”、“本地可靠资源”、“业务引用与关闭托管”和“远程汇合/Outbox”。

- [ ] **Step 2: 实现三个私有方法**

```go
func (s *Service) newOrderWriteStore(sc *router.ServiceContext) (*transaction.OrderWriteStore, error) {
	basePath := orderPendingBasePath()
	badgerConfig := nosql.DefaultProductionConfig(basePath)
	badgerConfig.EnableLogger = false
	badgerConfig.AutoSync = false

	return transaction.NewOrderWriteStore(
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
}

func (s *Service) bindOrderWriteStore(sc *router.ServiceContext, store *transaction.OrderWriteStore) error {
	if err := store.UseWriteBehind(business.OrderWriteBehindTarget{}); err != nil {
		_ = store.Close(context.Background())
		return err
	}
	runtime := s.ensureRuntime()
	if err := runtime.Bind(store); err != nil {
		_ = store.Close(context.Background())
		return err
	}
	if err := sc.UseResource("order-write-store", store); err != nil {
		runtime.Unbind()
		_ = store.Close(context.Background())
		return err
	}
	return nil
}

func (s *Service) startOrderInfrastructure(sc *router.ServiceContext) error {
	if err := sc.UseOutbox(models.OutboxStore{}); err != nil {
		return err
	}
	s.startPendingSync(sc, s.ensureRuntime())
	return nil
}
```

实际代码要在每个配置组和回滚段前补充设计文档规定的中文意图注释，但不对简单赋值作字面翻译。

- [ ] **Step 3: 运行 Service 测试并确认 GREEN**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/07-shop-order-scale/order-service -run 'TestNewOrderWriteStore|TestBindOrderWriteStore' -count=1
```

Expected: PASS，且失败资源注册后 runtime 已解绑。

- [ ] **Step 4: 补充 models 逐逻辑步骤注释**

`order_write_store.go` 必须解释：

```go
// NewOrderWriteStore 只向业务返回领域 store。Admin handle 被有意隔离，
// 避免普通业务通过 PurgeLocal 物理删除 pending 并跳过远程删除语义。

// Save 返回 nil 只表示订单已完成当前副本的 Badger SyncWrites，
// 进程异常后可恢复；它不表示 MySQL 权威库已经可见。

// Close 只停止本地提交并关闭 Order prefix。关闭路径不访问 MySQL，
// 未汇合 pending 保留在实例目录中，由关闭错误向运维层报告。
```

`order_write_runtime.go` 必须解释：

```go
// OrderWriteRuntime 在路由构造期就作为稳定引用注入 API/business，
// Service.Start 稍后绑定当前副本 store，因此不需要包级全局 registry。

// withStore 在完整委托调用期间持有读锁，保证 Stop/rollback 不会在
// 业务调用进行到一半时 Unbind。store 自身的并发安全仍由框架内部负责。
```

同时对 `PendingByUser`、`ForceSyncBatch`、`prepareForLocalInsert`、`Bind`、`Unbind` 和各委托方法添加同等密度的意图注释。`models/models.go` 只说明别名用于跨层兼容注入，不暗示根包拥有生命周期。

- [ ] **Step 5: 格式化并运行定向回归**

```bash
rtk gofmt -w examples/07-shop-order-scale/order-service
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/07-shop-order-scale/order-service/business ./examples/07-shop-order-scale/order-service/models/transaction ./examples/07-shop-order-scale/order-service -count=1
```

Expected: PASS。如本机直接 MySQL 用例被 `Error 1045` 阻断，单独运行不依赖 MySQL 的可靠写测试，并保留完整失败证据。

- [ ] **Step 6: 运行 race 和 buyer Docker UAT**

```bash
GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./examples/07-shop-order-scale/order-service ./examples/07-shop-order-scale/order-service/business ./examples/07-shop-order-scale/order-service/models/transaction -run 'TestNewOrderWriteStore|TestBindOrderWriteStore|TestRemoteOrderSyncer|TestOrderSyncer|TestOrderWriteBehindTarget|TestOrderWriteRuntime|TestOrderWriteStorePending' -count=1
SHOP_RUN_DOCKER_UAT=1 GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./examples/integration/07-shop-order-scale-multi-process -run '^TestDockerUATBuyerRoleFlow$' -count=1 -v
```

Expected: 定向 race PASS；buyer Docker UAT PASS。

- [ ] **Step 7: 检查注释与旧门面残留**

```bash
rtk rg -n 'globalOrderWriteStore|activeOrderWriteStore|StartOrderWriteStore|StopOrderWriteStore' examples/07-shop-order-scale/order-service
rtk git diff --check
```

Expected: 旧全局门面零命中，`git diff --check` 无输出。

- [ ] **Step 8: 提交实现**

```bash
rtk git add examples/07-shop-order-scale/order-service/service.go \
  examples/07-shop-order-scale/order-service/service_order_write_test.go \
  examples/07-shop-order-scale/order-service/models/transaction/order_write_store.go \
  examples/07-shop-order-scale/order-service/models/transaction/order_write_runtime.go \
  examples/07-shop-order-scale/order-service/models/models.go
rtk git commit -m "refactor: clarify order write assembly"
```
