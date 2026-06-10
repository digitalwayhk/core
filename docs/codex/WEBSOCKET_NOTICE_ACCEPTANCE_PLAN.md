# Core WebSocket / Notice 验收计划

## 验收场景

| 场景 | 标准 | 状态 |
| --- | --- | --- |
| public 订阅 | 可订阅/取消订阅，重复订阅幂等 | TODO |
| private 订阅 | token/user 校验正确，不能串用户 | TODO |
| hash 推送 | 只推送目标 market/user/symbol | TODO |
| 跨节点 notice | 多实例下不漏推、不重复推 | TODO |
| 节点下线 | draining 后连接迁移或可重连 | TODO |

## 交易系统重点

- 行情深度、ticker、kline 推送。
- 订单状态、余额、仓位风险私有推送。
