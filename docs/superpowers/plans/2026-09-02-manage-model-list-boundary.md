# Manage ModelList 分层边界实施计划

> **For Codex:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 修复 Manage 默认数据初始化绕过服务级 `GetList` 的生命周期问题，并让 Core 示例与现行 skill 统一遵守“Manage 只依赖 models 的 `NewManageModelList[T]()`”边界。

**Architecture:** Core 保留 `ManageService.GetList()` 的兼容默认值和 `entity.NewModelList[T](action)` 底层能力；服务在 models 持久化组合根集中选择 `IDataAction`，只向 `api/manage` 暴露无参数的 `NewManageModelList[T]()`。Manage 基类只在服务的最低公共抽象层重写一次 `GetList()`，更高层继承；特殊模型仅在连接权威确实不同且无法由模型路由表达时增加专用工厂。

**Tech Stack:** Go 1.24、泛型、`go/ast`、Testify、项目 `scripts/ci.sh` 验证门禁。

---

### Task 1: 修复默认数据初始化使用错误 ModelList

**Files:**
- Modify: `service/manage/request_isolation_test.go`
- Modify: `service/manage/manageservice.go:129-159`

- [ ] 在 `request_isolation_test.go` 添加回归测试：构造带自定义 `GetList()` 的最终 Manage owner，让默认数据初始化必须写入 `Search` 已解析出的 ModelList。
- [ ] 运行 `go test ./service/manage -run TestSearchAfterUsesResolvedSearchModelList -count=1`，确认旧实现因自定义 DataAction 未收到写入而失败。
- [ ] 将 `SearchAfter` 的默认数据保存目标改为 sender `*Search[T]` 已持有的 ModelList；sender 不是 Search 时保留兼容回退。
- [ ] 重新运行该测试及 `go test ./service/manage -count=1`，确认通过。

### Task 2: 建立服务 models 唯一 Manage ModelList 入口

**Files:**
- Modify: `examples/01-simple-shop/models/data_action.go`
- Modify: `examples/02-shop-payment/models/data_action.go`
- Modify: `examples/03-shop-inheritance/models/data_action.go`
- Modify: `examples/04-shop-performance/models/data_action.go`
- Modify: `examples/05-shop-casdoor-rbac/models/models.go`
- Modify: `examples/06-shop-microservices/{user-service,supplier-service,order-service}/models/models.go`
- Modify: `examples/07-shop-order-scale/{supplier-service,order-service}/models/models.go`
- Modify: `examples/01-simple-shop/api/manage/{productmanage.go,ordermanage.go}`
- Modify: `examples/02-shop-payment/api/manage/{productmanage.go,paymenttypemanage.go,ordermanage.go,paymentrecordmanage.go}`
- Modify: `examples/03-shop-inheritance/api/manage/shop_manage.go`
- Modify: `examples/04-shop-performance/api/manage/shop_manage.go`
- Modify: `examples/05-shop-casdoor-rbac/api/manage/common/shop_manage.go`
- Modify: `examples/06-shop-microservices/{user-service,supplier-service,order-service}/api/manage/common/service_manage.go`
- Modify: `examples/07-shop-order-scale/{supplier-service,order-service}/api/manage/common/service_manage.go`
- Modify: `examples/07-shop-order-scale/order-service/api/manage/transaction/order_manage_test.go`
- Add: `internal/compat/manage_model_list_boundary_test.go`

- [ ] 先添加静态架构测试，扫描 `examples/**/api/manage/*.go`，拒绝直接调用 `entity.NewModelList`、引用 `IDataAction` 或调用 `*DataAction`；同时要求所有含 Manage API 的服务 models 包声明 `NewManageModelList`。
- [ ] 运行 `go test ./internal/compat -run TestExampleManageModelListBoundary -count=1`，确认现有 07 order 直接 DataAction 用法导致失败。
- [ ] 在每个服务 models 组合根新增无参数泛型 `NewManageModelList[T]()`；使用统一的详细中文注释，内部调用本服务私有/既有 store 入口。
- [ ] 在每个服务最低公共 Manage 抽象层重写 `GetList()`；01/02 没有公共基类时在具体 Manage 实现中重写。删除 Manage 对 `entity` 和 DataAction 的感知。
- [ ] 更新 07 order 测试，只断言 `GetList()` 与 `models.NewManageModelList` 选择同一权威 DataAction。
- [ ] 运行架构测试和各示例包测试，确认通过。

### Task 3: 修正文档与 skill，明确分层契约

**Files:**
- Modify: `docs/ai/core-skill/SKILL.md`
- Modify: `docs/ai/core-skill/manage.md`
- Modify: `docs/ai/core-skill/models.md`
- Modify: `docs/ai/core-skill/project-layout.md`
- Modify: `docs/ai/core-skill/common-mistakes.md`
- Modify: `internal/compat/docs_contract_test.go`

- [ ] 先在文档契约测试中加入断言：现行 skill 必须推荐 `NewManageModelList`，且不得再指导 `api/manage` 传入 `ManageDataAction`/`RemoteDataAction`。
- [ ] 运行 `go test ./internal/compat -run TestCoreSkill -count=1`，确认旧文档导致失败。
- [ ] 修改 skill 总契约、Manage 指南、models 指南、目录指南和常见错误，写清 Manage→models→DataAction 的单向边界、注释模板、普通分库与特殊权威库的判断标准。
- [ ] 重新运行文档契约测试，并运行 skill 中相关代码片段的搜索校验。

### Task 4: 格式化、完整验证与提交

**Files:**
- Modify: 本计划涉及的全部 `.go`、`.md` 文件

- [ ] 对改动 Go 文件运行 `gofmt -w`。
- [ ] 运行 `go test ./service/manage ./internal/compat -count=1`。
- [ ] 运行所有受影响示例的 `go test ./...`（分别在各示例 module 根目录执行）。
- [ ] 按仓库要求运行 `scripts/ci.sh` 中适用的完整门禁；若环境门禁不可运行，记录准确命令与阻塞证据，不得假绿。
- [ ] 审查 `git diff --check`、`git diff` 和 `git status --short`，确保没有 Bitzoom 文件或无关改动。
- [ ] 将实现提交到 `core-codex/main`；提交前重新核对原始主 Core checkout 的分支、共同祖先和文件重叠，再执行用户授权的合并并验证结果。
- [ ] 只读扫描 `/Users/vincent/orca/workspaces/bitzoom`，产出逐文件改造清单和顺序，不修改 Bitzoom。
