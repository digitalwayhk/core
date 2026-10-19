# 07 业务下单长尾专项（实施中）

本专项继续执行[容量定位计划](../superpowers/plans/2026-10-19-order-capacity-focused-diagnosis.md)，当前没有需要用户裁决的阻塞项。Manage API 只在计时外准备规则，不设管理 API 吞吐目标；压力入口始终是业务 `POST /api/shop-user/addorder`。

## 无 flight 的外部取证轮

证据：`.temp/order-tail-rootcause-20261019/performance/external-gc-core-r1/`。源码 `7c26f5ed90ac8e10507d8648372ac5af7162542d`，全新 MySQL/Redis 卷及本地状态，MySQL buffer pool 1 GiB、提交刷盘/binlog 同步均为 1；Redis AOF everysec。预热 500/s × 10 秒，主窗 2000/s × 600 秒，512 在途、5 秒请求超时、原积压及连续观测失败守护保持。保留运行中的 19 个 Bitzoom 容器，属于共享主机诊断，不能当独占容量认证。

- 主窗完整运行，无守护停发、无原负载观测错误。977043 个确认请求逐键匹配，缺失/错 ID 为 0；1166 个失败请求实际落库，继续单列为结果未知。
- 主窗成功 972043；500 为 27861，非 JSON 503 为 95387，另有运输错误/发送器丢弃，未达容量目标。原生 go-zero HTTP breaker 的拒绝日志证明部分非 JSON 503 来自熔断，不能把它们都解释成 stable 准入或 Redis 故障。
- 完整排空 118.299667 秒，包含本地与 SQL unpublished 的 `store_drain_seconds` 为 117.300225 秒；不能将后者解释成仅本地 pending 排空。最终 order/outbox/published 汇合，Broker lag/PEL 为 0，用户 facade 非持久 Inbox 的边界不变。
- 独立 Python Redis PING 共 3041 次，错误 0，最大约 142.9 ms。三进程 metrics 探针业务窗口无超时，最大约 395.5 ms；退出时的连接错误必须与业务窗口分开。一次外部延迟触发系统采样和 SIGUSR1，三份系统采样退出码均为 0，证据含精确 PID/祖先关系/二进制 SHA。采样本身有扰动。
- 能完整解析的 GC 行中，user/supplier/order 最大两段 STW 时钟之和分别约 124.027/84.007/33.8 ms。没有捕获到此前秒级 STW 的同因证据；gctrace 与业务日志共用文件，不能把已解析行的最大值升级成完整运行时证明。
- Badger 原件与关闭副本 SHA 相同。只读打开要求 WAL 恢复后，仅恢复新副本，再只读核对成功；原件 SHA 不变。源码 manifest 无变化；失败卷、本地状态、请求和诊断均保留。

## 已定位的负载放大

`post-window-statement-digests.tsv` 中，缺失默认规则查询执行 **996142 次**，远程请求幂等查询执行 **994078 次**。07 与独立原生对照均在规则缺失时返回内建默认值且不负缓存，保持后续管理员新增规则可见；不得为了压测擅自负缓存改变该语义。

订单连接池最终累计等待 687982 次、52292.103 秒，这是多请求累计等待，不是壁钟。最大本地 pending 34030，已观测磁盘指标最大约 44.5 MB。准入尝试 1005592、本地提交 978210，两者差值不能在缺少分类计数时直接当成某一种拒绝原因。

本轮实时指标另确认 Outbox `batch_limit=100`、`key_concurrency=4`。底层 EventBridge 已支持 BatchSize，但 ServiceContext 的公开运行时选项目前只转发 KeyConcurrency。每批查询与确认的固定开销是下一项独立验证方向，不能将增大并发或批量未经测量地当成修复。

## 当前推进

1. 增加三个已有准入累计计数的只读 Runtime 暴露：并发、积压、磁盘拒绝。单测先得到缺指标的行为 RED，再验证快照和真实 Prometheus Collector，继续过滤高基数名称；不调整准入阈值。
2. 用显式 `CORE_TEST_LOAD_SEED_RULE=1` 在预热前调用一次既有 Manage API，配置来自 `models.NewOrderRule()` 的相同默认规则。Core 与原生替换前共用该权威事实，记录无凭证的 fixture 文件；不压测或优化 Manage API。
3. 先在原 Outbox 批量 100/并发 4 下运行配置完整的诊断对照，再决定是否单独验证批量大小。保持刷盘、幂等、稳定身份、失败屏障和最终逐键核对，不降低验收门槛。

## 上游运行时线索

2026-10-19 查询的 Go [问题 #81759](https://github.com/golang/go/issues/81759)及[候选修复 #81835](https://github.com/golang/go/pull/81835)仍开放，与此前独立 pipe/GC 复现相近，但没有证明本轮业务同因。上游指出 `schedtrace` 会掩盖其复现，因此本轮仅 `gctrace=1`，不加入忙定时器，不修改全局 Go 工具链。后续若启用 FlightRecorder，必须单列观测扰动并验证 trace 解码，不把未复现当修复。

## 一次性配置夹具纠正

`configured-rule-core-r1` 在业务发压前因 Manage 403 停止，没有生成容量样本。原因是夹具复用了供应商管理员数字 UID，而订单服务 `OnAuthRequest` 要求 `contract.PlatformAdminUserID`（`platform-admin`）。改为引用现行契约常量，不改鉴权实现、不跨认证域。失败轮的新卷及日志保留，下一轮仍从新空存储启动；本次失败不归因于 Manage 性能。
