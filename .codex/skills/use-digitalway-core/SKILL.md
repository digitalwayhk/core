---
name: use-digitalway-core
description: 当任务涉及 github.com/digitalwayhk/core 的服务、IRouter、ModelList、Manage CRUD、认证、WebSocket、Cluster、Transport、MQ/EventBridge、配置、测试或框架消费方兼容性时使用。
---

# 使用 Digitalway Core

## 开始前

Digitalway Core 是 go-zero 与成熟依赖之上的应用组装框架。先读当前仓库的 `README.md`、最近示例和目标包；不要从已删除的 Copilot skill、旧文档或记忆恢复行为。

按任务读取：

- API、模型、Manage、启动与版本引用：`references/core-backend-api.md`
- 场景与成熟度：`docs/codex/FRAMEWORK_USAGE_GUIDE.md`
- 配置能力：`docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md`
- 日志与错误：`docs/codex/LOGGING_AUDIT_AND_STANDARD.md`

## 核心决策

1. 普通路由实现 `types.IRouter`；public/private 路径为 `/api/{service}/{structLower}`，目录决定认证但不进入 URL。
2. private 身份只读 `req.GetUser()`/claims，不信任请求字段。
3. Manage 使用 `NewManageService[T](owner)`，路径为 `/api/manage/{service}/{manage}/{operation}`。
4. 模型通过 `entity.NewModelList[T](nil)` 操作；嵌入 `*Model`/`*BaseModel` 必须在 `NewModel()` 初始化。没有稳定 `Code` 时不用 `BaseModel`。
5. 先复用 go-zero/成熟客户端；Digitalway 抽象只保留路由、模型、MachineID、Provider 切换、事件和跨节点通知等领域契约。
6. 配置字段不等于支持。`Unsupported` 值必须 fail closed；QUIC/MQ transport 和内建 Kafka/RabbitMQ/RocketMQ 不得伪装可用。
7. CORS origin、TrustedProxies 和外部依赖必须显式配置。默认单元测试不依赖 Docker。
8. 日志使用 `logx` 稳定事件和字段；不记录 token、TOTP、payload/body/response、SQL、参数或对象 dump。
9. 修改公共 Go API、路由、JSON、配置或错误前，运行兼容性/发布契约并登记迁移。

## 工作流

1. 找到最接近的 `examples/*` 或兄弟服务，核对当前构造器和测试。
2. 先写失败测试，再做最小实现；不绕过 ServiceContext、ModelList 或 Manage hook。
3. 对外部能力同时检查 config Validate、factory、真实启动链、lifecycle owner 和 integration gate。
4. 运行 `gofmt`、定向测试、`./scripts/check-logging.sh`；跨模块变更再运行 `release-contract` 和对应 race/CI gate。

## 审查重点

- URL 中错误加入 `/public` 或 `/private`。
- 请求身份/trace 存入共享单例。
- 模型嵌入指针未初始化，或无 Code 模型误用 BaseModel。
- ManageService 传错 owner，导致 hook 不执行。
- 静默接受不支持配置、重复制造连接池/重试/日志/队列。
- 外部测试默认连接本机服务，或失败后残留 goroutine、容器和锁。
- 日志/响应泄露内部错误和业务数据。
