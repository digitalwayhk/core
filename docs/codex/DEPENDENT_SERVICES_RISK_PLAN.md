# Core 依赖服务风险计划

## 依赖方

| 项目 | 依赖能力 | P0 风险 | 验收 |
| --- | --- | --- | --- |
| futures | decimal、ModelList、service wrapper、Redis/MQ、WebSocket notice | 金额精度、用户/市场解析、漏推/错推 | orderflow + WebSocket smoke |
| omni-flow-ai-grok | core server、ManageService、SQLite/持久化、审计模型 | GetHash/查询兼容、管理 CRUD、trace 审计 | MVP tests + 管理后台 build |
| ai-ops-platform | 间接依赖交易系统和核心观测接口 | 交易服务健康/指标/事件不完整 | 探针和告警验收 |

## 兼容矩阵

| 项 | core-codex | futures | omni-flow | ai-ops | 状态 |
| --- | --- | --- | --- | --- | --- |
| Go 版本 | 1.26.0 | 待确认 | 待确认 | Node 20 + TS | TODO |
| 路由模式 | public/private/manage | core wrapper | core service | Fastify/Next | TODO |
| 数据存储 | ModelList/SQLite/MySQL | BadgerDB/MySQL/ClickHouse | SQLite MVP | Postgres | TODO |
| 消息 | Event/MQ/Notice | Redis Streams/PubSub | 后置 | 监控事件 | TODO |
