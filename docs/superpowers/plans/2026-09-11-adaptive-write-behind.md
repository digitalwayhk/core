# 自适应 write-behind 实施计划

> 执行方式：使用 superpowers:executing-plans，按仓库指令在主线程顺序实施，不分派代理。

**目标：** 实现已批准的阈值/收集期限/积压排空策略，零值兼容旧 worker。

**架构：** 在 BadgerDBConfig 增加三个标量配置；私有调度器计算下一次可执行时间，worker 复用已有同步事务与互斥。pending 计数锁同时保护空闲周期起点。

**技术：** Go、Badger、现有 WriteBehindTarget、MySQL 隔离测试。

## 1. 配置和调度 RED → GREEN

- [x] 新增 `pkg/persistence/database/nosql/sharedbadger_adaptive_test.go`：通过 JSON 输入新配置，断言无效组合被 Validate 拒绝，旧实现会错误接受。
- [x] 执行 `go test ./pkg/persistence/database/nosql -run TestAdaptive -count=1`，记录 RED。
- [x] 修改 `badgerdbconfig.go`：新增 `SyncFlushThreshold int`、`SyncMaxCollectDelay time.Duration`、`SyncBacklogDrainDelay time.Duration`，校验三字段全零或完整有效组合。
- [x] 新增 `sharedbadger_adaptive.go`：私有状态保留下一次重试时刻与连续失败退避；收集期限使用首条 pending 时刻，阈值提前；成功且剩余直接排空，错误/零进展按 min/max interval 退避。
- [x] 用固定时间输入验证状态转移，不依赖真实 sleep：检查 `delay(now, count, first)` 与完成反馈后 delay 的精确值。

## 2. 真实 Badger 接线

- [x] 先增加真实 store 测试：阈值唤醒、同 key 合并、恢复积压、失败保留、新信号不得绕过退避、关闭中断。
- [x] `sharedbadger.go` 在 pending 从零变正时记录时间，归零时清除；启用新策略时分流到新 worker，零值旧路径不改。
- [x] 新 worker 使用可关闭 timer，不增加并发同步；复用 `processSyncQueue`，保留 syncExecMu 和条件 ACK。恢复索引不与实时计数互相覆盖。
- [x] 执行 `go test -race ./pkg/persistence/database/nosql -count=3`，检验既有可靠写/手动同步/关闭回归。

## 3. 文档与验证

- [x] 更新 `docs/ai/core-skill/write-path-and-performance.md`、`docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md`、`CHANGELOG.md`；新增 `docs/codex/WRITE_BEHIND_SYNC_GUIDE.md`，明确三字段全零兼容、失败退避和端到端时延边界。
- [x] 运行 `gofmt`、`persistence-unit`、`performance-contract`、`config-contract`、`release-contract`、日志与 skill 检查。
- [x] 使用隔离真实 MySQL 对照 100 ms / 10 ms / 自适应，记录负载与事务/批次数据；外部环境无法执行时明确 NOT RUN，不将 mock 指标等同 redo/fsync。
- [x] 审查差异和证据，提交实现但不推送、合并或发布；保留 worktree 供复核。
