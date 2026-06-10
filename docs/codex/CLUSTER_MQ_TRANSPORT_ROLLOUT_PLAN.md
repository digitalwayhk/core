# Core Cluster / MQ / Transport 上线计划

## 决策

- 开发默认可使用 Redis Streams/local provider。
- 生产推荐对关键事件评估 NATS JetStream 或具备重放/ack/dlq 能力的 provider。
- 所有 provider 必须具备可观测指标、超时、重试、死信和降级策略。

## 验收场景

| 场景 | 标准 | 状态 |
| --- | --- | --- |
| request/reply | 超时、成功、失败均可观测 | TODO |
| publish/subscribe | 消息不丢、可重放或有补偿 | TODO |
| provider 切换 | 配置变更可控，失败可回滚 | TODO |
| 节点上下线 | draining 不丢事件 | TODO |
