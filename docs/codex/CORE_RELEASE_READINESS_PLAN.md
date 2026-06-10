# Core Codex 2026-06 发布准备计划

> 目标：为 futures、ai-ops-platform、omni-flow-ai-grok 提供稳定的核心框架能力，降低路由、持久化、管理 CRUD、MQ、WebSocket、集群和传输层风险。

## 当前结论

- 核心框架承担路由、认证、传输、持久化、管理 CRUD、WebSocket、MQ/Cluster 等基础能力。
- `go.mod` 声明 `go 1.26.0`，依赖项目和 CI/CD 构建镜像必须对齐。
- 单测覆盖面较广，但 etcd/Consul/Redis/NATS 等外部 provider 测试默认通过环境变量 skip。
- 当前仓库已有既有整理变更：根目录 Copilot 文档删除、`docs/copilot/` 新增，Codex 不应回滚。

## P0 缺口

| ID | 缺口 | 执行任务 | 验收标准 | 状态 |
| --- | --- | --- | --- | --- |
| CORE-P0-001 | Go 工具链兼容风险 | 确认 futures/omni-flow/ai-ops 的 Go/Node 构建镜像和 core 版本兼容 | 兼容矩阵完成 | TODO |
| CORE-P0-002 | 外部 provider 测试默认 skip | 提供 Docker 本地环境或文档化测试条件 | Redis/NATS/etcd/Consul 测试可按需运行 | TODO |
| CORE-P0-003 | WebSocket/Notice 多节点风险 | 验证订阅 hash、私有鉴权、跨节点 notice、节点下线 draining | WebSocket 验收脚本通过 | TODO |
| CORE-P0-004 | Manage/Persistence 兼容风险 | 验证 ModelList、GetHash、ManageService、自定义 Operation | demo 与依赖项目 smoke 通过 | TODO |
| CORE-P0-005 | MQ/EventBridge 上线策略 | 明确 Redis Stream 与 NATS JetStream 生产推荐配置 | 配置和切换文档完成 | TODO |

## 验证命令

```bash
cd /Users/vincent/Documents/存档文稿/MyCode/digitalway.hk/core-codex
rtk go test ./...
rtk go test -short ./...
rtk go test ./pkg/server/...
rtk go test ./pkg/persistence/...
rtk go test ./service/manage/...
CORE_TEST_CLUSTER_LOCAL=1 rtk go test -tags=integration ./tests/integration/... -run TestClusterLocal
CORE_TEST_REDIS_STREAM=1 CORE_TEST_NATS=1 rtk go test -tags=integration ./tests/integration/... -run TestMQ
CORE_TEST_CLUSTER_LOCAL=1 rtk go test -tags=integration ./tests/integration/... -run 'Test.*WebSocket|TestCrossNode'
```

## Agent 执行策略

- `core-compat-agent`：Go 版本、依赖兼容、依赖项目风险矩阵。
- `core-provider-agent`：Redis/NATS/etcd/Consul 集成测试和 Docker 验证。
- `core-ws-agent`：WebSocket、SubscribeRouters、notice、draining 验收。
- `core-manage-agent`：ModelList、ManageService、admin CRUD 兼容。
