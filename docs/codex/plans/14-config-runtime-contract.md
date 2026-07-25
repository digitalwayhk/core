# 配置到运行时能力契约实施计划

> **完成态记录：** 14.1-14.4 已完成，修复提交 `c52e32e` 的外部复审结论为 APPROVED，关闭提交为 `71118b3`。下文步骤仅保留作历史审计，不是当前执行指令。

**目标：** 让框架接受的每个项目自有配置字段都有明确默认值、校验、运行时消费方、生命周期归属和行为测试；尚未实现的能力必须在启动前返回可操作错误，不能静默跳过。

**原则：** 继续使用 go-zero、官方 driver 和现有 Cluster/Transport/MQ 组件，框架只负责配置组装、能力声明和生命周期。配置兼容优先采用 `Mode=off` 保留旧文件、启用时 fail-fast，不为未使用字段制造占位实现。

**关闭态兼容：** `Cluster.Mode=off` 与 `MQ.Mode=off` 只校验 Mode 本身，其他扩展字段按原样保留且不做语义校验。关闭态用于旧 JSON 禁用后迁移，不代表其中的 provider、usage 或能力已经受支持；切换到 `auto/on` 时必须通过完整语义校验。

## 14.1 建立字段级能力矩阵

- [x] 创建 `docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md`。
- [x] 记录项目自有 Server/Auth、Cluster、Transport、MQ 和 persistence 边界。
- [x] 区分 `supported`、`rejected` 与 `upstream`，不允许静默忽略状态。

**完成记录（2026-07-12）：** 已建立并校准最终矩阵。状态收敛为 supported/rejected/upstream；机器检查清单由反射门禁逐字段核对，未接线字段明确拒绝，inactive/default 仅作为旧配置兼容说明。

## 14.2 通过生产构造器验证启动和关闭

**文件：**
- 修改：`pkg/server/router/servicecontext.go`
- 修改：`pkg/server/router/servicecontext_*_test.go`
- 修改：`pkg/server/mq/manager.go` 及其测试（若生命周期需要）

- [x] 使用 `NewServiceContextWithConfig` 的生产初始化路径验证 ApplyDefaults/Validate。
- [x] 验证 ClusterProvider、TransportSelector、MQManager、EventStream/EventBridge 按配置创建。
- [x] 验证运行态启动 membership 和 CrossNodeNoticeBroker。
- [x] 验证停止时 broker、membership 和由 ServiceContext 拥有的 MQ provider 有界关闭；重复停止幂等。

**完成记录（2026-07-12）：** 生产构造器测试覆盖默认化、启动顺序和 runtime 装配。ServiceContext 对 MQManager/provider、membership 与 CrossNodeNoticeBroker 执行终止型关闭；关闭后对象不承诺复用，重复关闭保持幂等。

**验收：**
```bash
go test ./pkg/server/router ./pkg/server/mq -run 'Test.*(Config|Lifecycle|Close|EventBridge)' -count=20
go test -race ./pkg/server/router ./pkg/server/mq -count=1
```

## 14.3 删除静默能力声明

**文件：**
- 修改：`pkg/server/config/clusterconfig.go`
- 修改：`pkg/server/config/transportconfig.go`
- 修改：`pkg/server/config/mqconfig.go`
- 修改：对应 config/factory 测试

- [x] Transport 的 `quic`、`mq` 作为 internal 或 fallback 时在 Validate 阶段失败。
- [x] MQ 未实现 provider 在 BuildManager 阶段失败；未知 Usage 及尚未接线的 request/reply、retry、dead-letter、dynamic-switch 在启用时失败。
- [x] Cluster 尚未接线的 discovery、shard、claim 策略及 Consul prefix/TTL 等字段在会误导运行时的组合下失败；Etcd Prefix 已接入 factory/provider 并支持自定义。
- [x] 错误包含字段路径、拒绝值和可操作的支持/替代说明。

**完成记录（2026-07-12）：** Cluster 固定默认值只用于兼容，改变不可配置值会被拒绝；Etcd Prefix 除外，默认 `/core/cluster` 并由 factory 透传给 provider，允许配置独立 keyspace。Transport 未实现协议和 enable 开关会被拒绝；MQ 内建未实现 provider 会被拒绝，但通过 `RegisterProviderFactory` 注册的自定义 provider 受支持。

**外部审查修复（2026-07-12）：** 补齐 Etcd Prefix 的运行时接线和无真实 etcd 的 key 构造/factory 回归测试；公共 `NewEtcdProvider(endpoints, ttl)` 保持兼容且继续使用既有 `/core/cluster` keyspace。Cluster factory 对未知 provider 改为配置错误，`Mode=auto` 的 local fallback 仅保留给已知 etcd/consul provider 的连接失败。

**外部审查修复第 3 节（2026-07-12）：** MQManager 的 Health/Publish/Subscribe 在 provider 注册调用期间持有生命周期读门禁，Close 取得写门禁并等待在途注册调用结束，关闭后新调用返回 `ErrNotConnected`；Subscribe 返回的用户 handler 不在门禁内执行。`Current` 仅返回快照，直接调用快照不受 Manager 门禁。provider 关闭按稳定 registry key/name 顺序执行，并按实例指针去重，同名不同实例仍分别关闭，`errors.Join` 顺序稳定。新增并发安全的 `UnregisterProviderFactory`，所有测试 factory 注册均通过 `t.Cleanup` 注销，避免跨测试污染。

**外部审查兼容性收尾（2026-07-12）：** 保留 `Mode=off` 关闭态迁移策略并在矩阵/计划中明确其余字段不做语义校验；Cluster 与 MQ 的拒绝错误统一携带实际值。删除 Transport 默认化中的不可达 HTTP.Enable 分支。MQManager 增加 `Current` 快照边界和异步 Subscribe handler 不阻塞 Close 的回归测试。

**验收：**
```bash
go test ./pkg/server/config ./pkg/server/cluster ./pkg/server/transport ./pkg/server/mq -count=1
```

## 14.4 添加配置变更门禁

**文件：**
- 创建：配置矩阵一致性测试或脚本
- 修改：`scripts/test.sh`
- 修改：`docs/codex/PROJECT_REVIEW_ACTION_PLAN.md`

- [x] 新增 `config-contract` 测试模式并设置 3 分钟 timeout。
- [x] 配置 struct 字段变更但矩阵未同步时测试失败；go-zero RestConf 只检查嵌入点。
- [x] 总计划记录本次提交占位、验收命令和明确拒绝项。

**完成记录（2026-07-12）：** 新增反射一致性测试，递归覆盖项目自有 ServerConfig/Auth/Logto/CasDoor、Cluster、Transport、MQ 字段，map/slice/interface/pointer 为叶子；矩阵禁止遗留静默状态。补充自定义 gRPC MaxRecv/MaxSend 经 ApplyDefaults 保持不变的回归测试。

**外部审查修复第 2 节（2026-07-12）：** 机器检查清单收紧为固定四列 Markdown 表，状态仅允许 supported/rejected/upstream，并要求每行的生命周期 owner 和运行时/拒绝证据非空且非占位值。门禁只解析该 section，拒绝非法行、重复 path 和正文伪命中；反射 paths 与矩阵 paths 做双向闭集差集校验。同包 pointer-to-struct 现在递归，map/slice 仍为叶子，go-zero RestConf 仍只跟踪嵌入点。

**最终门禁：**
```bash
./scripts/test.sh config-contract
go test -race ./pkg/server/config ./pkg/server/router ./pkg/server/cluster ./pkg/server/transport ./pkg/server/mq ./pkg/server/event -count=1
```

**最终门禁记录（2026-07-12）：** `config-contract` 与六包完整 race 均通过。外部只读审查发现的 Etcd Prefix 接线、矩阵门禁强度和 MQ Close 在途调用问题已按 TDD 修复；修复提交 `c52e32e` 复审结论为 APPROVED，关闭记录提交为 `71118b3`，任务 14 已完成。
