# 任务 17：性能、容量与运维 SLO 基线

## 目标

在 SharedBadger 拆分或并发策略调整前建立可重现基线，定义框架默认资源预算和服务 owner 可覆盖的 SLO。基准只用于趋势比较；在 CI runner 方差被测量前，不使用单次纳秒阈值阻断发布。

## 执行清单

- [x] 补齐 ServiceContext 注册表、LocalProvider、EventStream 和 WebSocket 队列 benchmark。
- [x] 复用并运行 SharedBadger 与 SQLite benchmark，记录三次测量区间和分配。
- [x] 将 SQLite `mmap_size` 从硬编码 30GB 改为默认 256MiB、按实例可配置、负值关闭，并添加契约测试。
- [x] 记录 goroutine、队列、连接、重试、缓存/映射、消息体和关闭预算及 owner。
- [x] 记录 HTTP/Event/MQ/Provider/WebSocket 的 RED/USE 信号与 trace 连续性要求。
- [x] 定义可用性、延迟、错误、投递和恢复 SLO，以及告警阈值和责任人。
- [x] 添加 `scripts/bench-baseline.sh` 和不依赖外部服务的验证命令。

## 验收

```bash
bash -n scripts/bench-baseline.sh
CORE_BENCH_TIME=200ms CORE_BENCH_COUNT=3 ./scripts/bench-baseline.sh
go test ./pkg/persistence/database/oltp -run TestSqliteMmapSize -count=1
go test -race ./pkg/server/router ./pkg/server/cluster ./pkg/server/event ./pkg/server/types -count=1
```
