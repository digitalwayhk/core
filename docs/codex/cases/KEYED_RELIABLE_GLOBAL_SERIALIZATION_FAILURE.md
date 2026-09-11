# 有序可靠被误实现为全局串行：Bitzoom Fill 容量失败案例

## 现象

Bitzoom 在同一自动采集环境中构造 100 个交易用户和 1048 条 Fill。业务已为 Fill设置稳定 `marketID:userID` ShardKey，Positions也有同用户 sequence cursor，但端到端仍出现：

- `Fill created -> Outbox published` p95 `76.427s`、p99 `80.169s`；
- Trades 单市场 Store 每次只返回最早一条 Fill；
- Core Outbox逐条 Publish/MarkPublished（未实现可选 `OutboxBatchMarker` 时仍如此；实现后仍逐条 Publish，只对成功前缀一次确认）；
- Redis reliable订阅强制 Count=1，并在读取新消息前串行排空 PEL。

契约没有丢消息或同 key越序，却因为 Store、publisher、consumer三层都把“有序可靠”解释成全局串行，形成约 30 orders/s 即可触发的容量失败。

## 根因

OrderingKey 已存在，但执行器没有把它作为并发隔离单元。只保证“同 key串行”并不要求“不同 key也串行”；可靠 ACK也不要求一个 subject永远只有一个 handler in-flight。

## Core 修复模式

1. 默认保持 worker/concurrency `1`，不改变既有调用方跨 key完成顺序。
2. 显式 opt-in 后，Outbox按 key建 lane；同 key 仍串行 Publish，失败才阻断后续。未实现 `OutboxBatchMarker` 时仍是 Publish 后逐条 `MarkPublished`；实现后对已发布前缀一次确认，再进入下一轮 drain。
3. Redis继续保持单 active owner，但 owner内按 OrderingKey有界并行；PEL分页扫描，poison key只阻断自身。
4. owner接管允许同 EventID短暂重复；消费者必须用 Inbox、业务幂等键和 sequence cursor收敛。
5. Store必须按 key公平组成 batch。Core scheduler不能补救一个永远只返回 hot key的 Store。

## 禁止的推论

- 不能因为 Core conformance通过就跳过业务 MySQL、Inbox、sequence和恢复测试。
- 不能把 key concurrency设为默认大于 1。
- 不能承诺跨 owner exactly-once或不同 key的全局完成顺序。
- 不能靠调大 timeout掩盖 backlog。

## 认证证据

- provider-neutral：同 key 100条顺序、不同 key实际重叠、poison-key隔离、cancel/race。
- 真 Redis：pending接管、owner fencing、ACK后置、不同 key并行、失败恢复。
- consumer：同一环境/数据集比较每条异步边，保存 count/p50/p95/p99/max和 missing watermark。
