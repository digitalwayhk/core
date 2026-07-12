# 配置到运行时能力契约实施计划

> 面向智能体开发者：按小节执行 TDD。每节先观察失败测试，再做最小实现和定向验收；代码完成后由外部审查 Agent 统一审查。

**目标：** 让框架接受的每个项目自有配置字段都有明确默认值、校验、运行时消费方、生命周期归属和行为测试；尚未实现的能力必须在启动前返回可操作错误，不能静默跳过。

**原则：** 继续使用 go-zero、官方 driver 和现有 Cluster/Transport/MQ 组件，框架只负责配置组装、能力声明和生命周期。配置兼容优先采用 `Mode=off` 保留旧文件、启用时 fail-fast，不为未使用字段制造占位实现。

## 14.1 建立字段级能力矩阵

- [x] 创建 `docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md`。
- [x] 记录项目自有 Server/Auth、Cluster、Transport、MQ 和 persistence 边界。
- [x] 区分 `supported`、`rejected`、`accepted-but-ignored` 与 `upstream`。

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
- [x] Cluster 尚未接线的 discovery、shard、claim 策略、provider prefix/TTL 等字段在会误导运行时的组合下失败。
- [x] 错误包含字段路径和可操作的支持/替代说明。

**完成记录（2026-07-12）：** Cluster 固定默认值只用于兼容，改变不可配置值会被拒绝；Transport 未实现协议和 enable 开关会被拒绝；MQ 内建未实现 provider 会被拒绝，但通过 `RegisterProviderFactory` 注册的自定义 provider 受支持。

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

**最终门禁：**
```bash
./scripts/test.sh config-contract
go test -race ./pkg/server/config ./pkg/server/router ./pkg/server/cluster ./pkg/server/transport ./pkg/server/mq ./pkg/server/event -count=1
```

**最终门禁记录（2026-07-12）：** 以上两条命令为任务 14 最终本地门禁。`config-contract` 不依赖外部服务；外部审查由后续独立 Agent 执行，本计划不声称其已经通过。
