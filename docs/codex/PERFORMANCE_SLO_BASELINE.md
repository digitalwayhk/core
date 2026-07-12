# 性能、容量与 SLO 基线

## 测量环境

- 日期：2026-07-13
- 平台：macOS darwin/arm64，Apple M3 Max，16 逻辑 benchmark worker
- Go：当前 `go.mod` 声明版本对应的本机工具链
- 参数：`-benchmem -benchtime=200ms -count=3`
- 命令：`CORE_BENCH_TIME=200ms CORE_BENCH_COUNT=3 ./scripts/bench-baseline.sh`

这些结果是本机趋势起点，不是跨机器承诺。比较必须使用相同工具链、机器类别、benchtime 和 count；结构性优化应附 `benchstat` 或等价统计，至少 5 次样本。

## 基线结果

| 路径 | 三次区间 | 分配 | 说明 |
| --- | --- | --- | --- |
| ServiceContext 注册表并行 lookup | 109.1-120.1 ns/op | 0 B/op，0 allocs/op | 读锁下命中 |
| LocalProvider 列出 100 个运行节点 | 5.58-5.82 us/op | 19,713 B/op，105 allocs/op | 返回防变更副本；优化不得破坏隔离 |
| EventStream 发布到 10 个订阅者 | 133.1-135.8 ns/op | 80 B/op，1 alloc/op | 同步投递和 handler 切片快照 |
| WebSocket 通知队列提交/取出 | 20.17-20.59 ns/op | 0 B/op，0 allocs/op | 不包含过滤和网络发送 |
| SharedBadger 顺序 Set | 8.13-15.55 us/op | 约 3,328 B/op，71 allocs/op | 短 benchtime 下波动较大，不设硬阈值 |
| SharedBadger 顺序 Get | 2.55-3.71 us/op | 约 2,287 B/op，45 allocs/op | 本地纯 Badger 路径 |
| SQLite Insert | 42.7-47.0 us/op | 约 6,503 B/op，87 allocs/op | 临时数据库、WAL |
| SQLite Query | 104.4-121.0 us/op | 约 16,198 B/op，321 allocs/op | 当前 ModelList/GORM 查询路径 |

LocalProvider fixture 必须为每个节点设置唯一 DataCenterID/MachineID；否则 benchmark 会正确触发隔离冲突，而不是测量 List。

## 资源预算

| 资源 | 默认/上限 | Owner | 超限动作 |
| --- | --- | --- | --- |
| SQLite mmap | 默认 256MiB/实例；`MmapSize < 0` 关闭 | Core 持久化维护者 | 拒绝恢复机器级 30GB 默认；按工作集测量后覆盖 |
| SQLite 连接 | max open=2、idle=1、lifetime=5m、idle time=2m | Core 持久化维护者 | 连接等待/锁冲突升高时先 profile，不盲目扩池 |
| WebSocket 通知 | 20 worker、队列 10,000、filter 1s、shutdown 5s | Core 实时通信维护者 | 队列 >90% 或 drop >10% 告警；扩容前检查慢 filter |
| Transport retry | 配置 `MaxRetries`；单次退避最多 5s | 服务 owner，Core 提供边界 | attempt 为 debug，耗尽后一次 legacy HTTP fallback |
| 集群心跳/怀疑/复用 | 由 ClusterConfig 校验并由 Membership owner 关闭 | Core 集群维护者 | 心跳最终失败和 provider degraded 告警 |
| HTTP body | 使用 ServerConfig/REST 已验证上限 | 服务 owner | 超限返回客户端错误，不读入内存后再判断 |
| 外部连接池 | 使用成熟 driver/go-zero 客户端配置 | 服务 owner | 以等待、使用率、错误和慢操作决定容量 |
| SharedBadger 本地映射 | 由 prefix/auto-limit 管理；禁止无界业务 key | Core 持久化维护者 + 服务 owner | pending、磁盘、同步延迟达到阈值时限流/告警 |

任何默认值变更都必须附 benchmark、峰值内存/连接/goroutine 证据和回滚说明。

## RED 与 USE 信号

| 边界 | RED/USE 信号 | 低基数维度 | 禁止维度 |
| --- | --- | --- | --- |
| HTTP 路由 | request rate、error rate、p50/p95/p99 duration、slow count | service、route template、method、status class | user id、原始 path/query、body |
| Provider/Transport | operation rate、error、duration、retry、fallback、switch state | provider、operation、service | endpoint 完整 URL、payload |
| MQ/Event | publish/consume rate、error、duration、ack/nack、queue lag | provider、subject family、event type | message id、body、tenant raw id |
| WebSocket | active connections、queue usage、drop、filter timeout、shutdown failure | service、route、shard | connection id、message body |
| 持久化 | pool in-use/wait、operation duration/error、pending sync、disk bytes | driver、database role、operation | SQL、参数、record key/value |
| 进程 | goroutine、heap、GC pause、CPU、file descriptor | service、instance class | request/tenant 标识 |

Trace 在 HTTP -> PayLoad -> Transport -> Event/MQ -> CrossNode 边界沿用同一 `trace_id`。日志只记录 trace_id 和稳定上下文，指标不使用 trace_id 作为 label。

## SLO 与告警

| 目标 | SLO | 告警阈值 | Owner |
| --- | --- | --- | --- |
| 框架 HTTP 可用性 | 月度成功率 >=99.9%，排除调用方 4xx | 5m error budget burn >14.4x 或 1h >6x | 部署服务 owner |
| 框架内部处理延迟 | router p95 <200ms、p99 <500ms，不含明确外部长任务 | 连续 10m 超 p95，按 route template 定位 | 部署服务 owner；Core 维护框架开销 |
| 内部事件投递 | 已接受消息 99.9% 在 5s 内 ack 或进入明确失败路径 | nack/drop >0.1% 或 lag 连续 10m 增长 | MQ/Event owner |
| Provider 恢复 | 可恢复故障 60s 内 ready/degraded 明确；切换不丢已确认成员 | degraded >5m、回滚失败立即告警 | Cluster/Transport owner |
| WebSocket 通知 | 队列 drop <0.1%，filter timeout <0.1% | 队列 >90%、drop >1% 持续 5m；>10% 立即严重 | 实时通信 owner |
| 优雅关闭 | 99.9% 实例在 10s 内完成；无丢失确认数据 | 任一 shutdown timeout/error | 服务 owner + 对应子系统 owner |

业务服务可以收紧目标，不得在没有容量与成本评估时放宽框架安全上限。告警应链接 `scripts/ci.sh` 或对应集成复现命令。

## 回归策略

1. PR required 只编译 benchmark 并运行资源契约，不按 ns/op 阻断。
2. 性能改动在同类 runner 上运行至少 5 次，并用统计工具比较 CPU、ns/op、B/op、allocs/op。
3. 只有方差稳定且有 owner 时，scheduled 才加入范围阈值；阈值至少保留 15% 噪声预算。
4. SharedBadger 文件拆分先证明 benchmark 等价；任何锁/队列语义变化必须重新跑 race、pending/CAS/fatal-break 契约。
5. 性能提升不得以返回共享可变对象、吞错误、扩大无界队列或跳过关闭为代价。
