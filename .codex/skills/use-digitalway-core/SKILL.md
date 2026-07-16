---
name: use-digitalway-core
description: Use when 使用或审查 github.com/digitalwayhk/core 的服务、IRouter、Model/Manage 继承、认证、WebSocket、缓存、本地可靠写、EventBridge、配置、集成测试、性能或兼容性时。
---

# 使用 Digitalway Core

## 定位

Digitalway Core 是 go-zero 与成熟依赖之上的应用组装框架。代码是最终事实，示例是用法模板，`docs/codex` 现行指南是边界与运维契约。不得从已删除的 Copilot skill、历史计划、审查提示词或记忆恢复当前行为。

具体 API、目录、模型、Manage、WebSocket、认证和测试模板见 `references/core-backend-api.md`。

## 示例选择

| 场景 | 标准示例 | 核心能力 |
| --- | --- | --- |
| 最简平台服务 | `examples/01-simple-shop` | contract、models、DTO、Manage/Public/Private、TestToken、用户 WebSocket、真实集成测试 |
| 业务编排与状态机 | `examples/02-shop-payment` | API -> business -> models、跨模型事务、支付状态机、受控 Manage 命令 |
| 模型和 Manage 继承 | `examples/03-shop-inheritance` | Shop/BaseData/Business 多层模型、Manage Hook 继承、只读子表、联合有效性 |
| 性能优化 | `examples/04-shop-performance` | RouterInfo L1/L2/L3、EventBridge 主动失效、SingleFlight、Badger 可靠本地写、Group Commit、基准与分位数 |
| Casdoor 身份生命周期 | `examples/05-shop-casdoor-rbac` | Auth/Manage 双域、三类 Hook、撤销世代、Webhook、幂等审计、领域分包与 facade |
| Redis 多服务 | `examples/06-shop-microservices` | 三服务边界、共享 DTO、Redis 发现、CallService、可靠 EventBridge、Outbox/Inbox、同进程/三进程测试 |

对应真实进程测试位于 `examples/integration/01-simple-shop`至 `05-shop-casdoor-rbac`，多服务还必须同时参考 `examples/integration/06-shop-microservices` 和 `06-shop-microservices-three-process`；通用进程、HTTP、TestToken 和 WebSocket 能力只复用 `examples/integration/helpers.go`。

## 目录决策

- 最简 CRUD 按 01 平铺 `models/api`；出现跨模型规则时增加无请求状态的 `business`。
- 出现基础资料、交易业务或身份审计继承时，按 03/05 拆分 `common/basedata/transaction/identity`；根包只做兼容门面。
- `contract` 必须无依赖；DTO 只放 `api/dto`；API 依赖 business，business 依赖 models，不得反向引用。
- 单元测试与实现同目录；跨子包契约测试留 facade 根包；真实进程测试只放 `examples/integration/<service>`；固定样本放 `testdata/`。

## 现行指南索引

| 任务 | 必读文档 |
| --- | --- |
| 场景和能力选择 | `docs/codex/FRAMEWORK_USAGE_GUIDE.md` |
| 配置是否真正接入运行时 | `docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md` |
| RouterInfo、对象池、EventBridge、缓存、WebSocket 和生命周期 | `docs/codex/ROUTERINFO_RUNTIME_GUIDE.md` |
| 日志级别、字段和敏感信息 | `docs/codex/LOGGING_AUDIT_AND_STANDARD.md` |
| Docker 外部依赖集成 | `docs/codex/EXTERNAL_INTEGRATION_GUIDE.md` |
| NATS JetStream 可靠写路径 | `docs/codex/NATS_JETSTREAM_WRITE_PATH_GUIDE.md` |
| 性能、容量、RED/USE 和 SLO | `docs/codex/PERFORMANCE_SLO_BASELINE.md` |
| go-zero 与成熟能力复用 | `docs/codex/GO_ZERO_REUSE_AUDIT.md` |
| 无用代码和架构债 | `docs/codex/DEAD_CODE_AUDIT.md`、`ARCHITECTURE_HARDENING.md` |
| 公共 API、废弃和消费方兼容 | `docs/codex/API_COMPATIBILITY_SURFACE.md`、`DEPRECATION_REGISTER.md`、`CONSUMER_COMPATIBILITY_MATRIX.md` |
| CI 和发布门禁 | `docs/codex/CI_QUALITY_GATE_MATRIX.md`、`docs/RELEASE_POLICY.md` |

`PROJECT_REVIEW_ACTION_PLAN.md`、`plans/`、`*_PROMPT.md`、`*_REVIEW.md` 和 `COMPLETED_TASKS_IMPLEMENTATION_REVIEW.md` 是历史审计证据，不是新实现的默认规范。

## 不可违反的契约

1. public/private URL 为 `/api/{service}/{router}`；Manage 为 `/api/manage/{service}/{manage}/{operation}`。
2. 服务名放无依赖 `contract`；RouterInfo 注册后 Path、ServiceName、Method、Auth 等元数据冻结，只通过 Getter 读取。
3. private 身份只读 `req.GetUser()`/claims，缓存键和 WebSocket 订阅不信任客户端 UserID。
4. Manage CRUD 不绕过 ModelList；public/private 不直接依赖 GORM/SQLite；`IDataAction` 实现只在 models 边界选择。
5. 模型嵌入指针必须在 `NewModel()` 初始化；`GetHash` 表达真实业务唯一性；引用后的基础资料只能禁用，不能删除。
6. public/private 返回独立 DTO 并实现 `GetResponse()`，不直接序列化深度继承的持久化模型。
7. WebSocket 只面向最终外部用户；内部服务通信使用 TransportSelector/EventBridge。
8. `UseCache` 是 API 级唯一启用声明；默认 local L1，L2/shared 才需显式配置；控制事件通过 EventBridge 主动失效。
9. Badger pending 是未同步业务事实，不是可丢弃缓存；高 TPS 写路径只能在本地持久成功后确认。
10. Casdoor Auth/Manage 是独立域，分离 Client、Access/Refresh/Webhook Secret；Callback、Refresh、REST 和 WebSocket 共享撤销权威。
11. 优先复用 go-zero/成熟客户端；不支持的配置值 fail closed，不得伪装可用。
12. 日志使用 `logx` 稳定事件和字段，不记录 token、payload/body/response、SQL、参数或对象 dump。
13. 修改公共 Go API、HTTP/JSON、配置或错误前后运行兼容/发布契约并登记迁移。
14. 跨进程调用直接构造目标 API，但 Go 目录名与稳定服务名不同时必须在注册前用 `WithServiceName` 和 `WithPath` 显式声明；地址只由 ClusterProvider + ServiceResolver 解析，新代码不读 `AttachServices`。
15. 跨服务控制事件使用逻辑服务消费组、可返回 error 的 Handler、成功后 ACK、pending reclaim 和 Inbox 幂等；业务事实与 Outbox 必须同事务。

## 工作流

1. 选择最近示例，再读对应现行指南，然后核对当前代码。
2. 先写失败测试；不绕过 ServiceContext、ModelList、模型持久化和认证生命周期。
3. 集成测试启动真实进程，使用自动生成配置、临时数据目录、真实 HTTP/WebSocket；普通业务用 TestToken，Casdoor 生命周期用 Fake Casdoor。
4. 运行 `gofmt`、定向测试、race、`./scripts/check-logging.sh`；跨模块变更再运行 `release-contract` 和对应 CI gate。

## 审查红旗

- RouterInfo 冻结后修改元数据，或在共享单例中保存请求、用户、trace、response。
- Manage owner 绑定错误、子类覆盖 Hook 却丢失必需父级规则，或用通用 CRUD 绕过状态机。
- public/private 直接返回持久化模型，DTO 混入公共测试 helpers。
- WebSocket 接受客户端 UserID、跨用户投递，或内部服务用 WebSocket 通信。
- `UseCache` 依赖全局开关、缓存键缺少身份/筛选维度、只靠 TTL 不主动失效，或把 write-behind pending 当缓存删除。
- 集成测试重复实现通用进程/TestToken/WebSocket 能力，只测 handler，或默认依赖 Docker/外部服务。
- 日志/响应泄露内部错误、Token、Claims、Header、请求或业务数据。
