# 独立调查：message lifecycle capacity reached

## 当前结论

这条错误是 Core 的**容量背压**，不能直接归因于可靠消费整主题单 owner。本轮在独立工作区和专用 Redis 调查并补真实回归，没有放大容量、切 observe、删消息/组或取消安全回收门禁。

`ErrLifecycleBackpressure` 由 `lifecycleController.allowPublish` 根据新鲜容量快照触发：`RetainedMessages >= HardMessages` 或 `RetainedBytes >= HardBytes` 即拒绝继续发布。过期/未知快照返回另一类 capacity unknown。pending 为零不等于 retained 为零，也不等于已经满足全部 required group 与最小保留期。

## 已验证机制

`TestLifecycleCapacityCountsRetainedHistoryNotOnlyPending` 在真实 Redis 声明 users/audit 两个必需组、硬上限 2 条、原保留期 250ms：

1. 发布 2 条，两组均读取，users 全 ACK，audit 仍 pending=2。
2. users pending=0，但发布返回 capacity reached；未完成的 audit 保护历史消息。
3. 原保留期自然结束后仍不得删除 audit 的未完成消息。
4. audit 也 ACK 后，由原安全回收 worker 回收原记录，发布恢复。

这证明正常安全门禁可以在某个服务 pending=0 时触发；不是框架丢消息、负 pending 或 owner 缺陷的证据。ACK 只完成组消费，物理回收还取决于全部必需组 completion frontier 与保留期。消息安全回收门禁应保留。

## 现场根因与修复边界

现场具体 subject、冻结 manifest、同一时刻的 retained 数/字节、全部组 lag/pending、回收错误/进度和容量快照未提供，**现场根因 NOT_CONFIRMED，生产诊断 NOT_RUN**。不能根据单行错误声称已修复，也不能推断增加消费者就一定解除容量。

需要在同一时刻只读关联：

- 实际应用声明的 RequiredGroups、MinAge、HardMessages/HardBytes、ReclaimBudget 和 MaxAckPending；不要从默认猜。
- Core MQ Runtime 中容量、快照新鲜度/完整性、publish_rejected、reclaim_failed/exhausted 及 provider 状态。
- Broker 保留消息/字节、每个 required group 的 pending 与 lag、最老未完成消息及 owner；只看 users 会遗漏 audit 等离线组。
- 回收 owner 是否进展、网络或策略指纹冲突、保留窗口内的已完成历史占比。

若证实回收器无法删除已经满足原策略的记录，再单独先 RED 后修安全回收实现。若是合法保留历史、离线组或业务处理失败，则由应用确认容量/保留/恢复目标，不能由 Core 擅自改变语义。本次交付的是已验证规则和诊断分离，不虚构现场修复。

复现：

```bash
CORE_TEST_REDIS_ADDR=127.0.0.1:<独占端口> go test -p 1 -race ./pkg/server/mq -run '^TestLifecycleCapacityCountsRetainedHistoryNotOnlyPending$' -count=1 -v
```

原始日志和最终普通/race 结果与共享消费证据分文件保留于 `.temp/shared-keys-20261020/`。
