# 稳定实例身份、生成器编号解耦与服务注册自愈实施计划

> 执行要求：使用 `superpowers:executing-plans` 按任务顺序实施；遵守项目 AGENTS.md，在主线程顺序执行。用户已授权复审、分支实施、实现再审计和隔离回归；没有授权生产迁移、部署或重启消费方。

**目标：** 服务注册以“服务名＋稳定实例 ID”标识副本；MachineID 独立、持久且不冲突；发现记录丢失后，运行进程能安全恢复注册和跨服务调用。

**架构：** 将持久实例身份、ID 生成器编号分配、带租约的运行期注册分成三个职责。ServiceContext 组合这些能力并管理退出和 Provider 切换；业务服务不编写重注册循环。复用现有客户端库与测试设施，编号和发现分别持有客户端，不引入新的基础设施依赖。

**技术栈：** Go、现有 Redis/etcd 客户端、现有 Snowflake、ServiceContext、MembershipManager、ServiceResolver；Consul 保留 legacy 兼容路径；真实多进程示例 06/07。

**状态：** 核心分支实现与本轮隔离回归完成；2026-10-18 计划复审修订为 v3，源码基线 HEAD `09f8ae3`。原始待办保留为历史计划，实际完成、未运行场景和现有 CI 缺口以 [实现审计报告](../../codex/STABLE_INSTANCE_IDENTITY_AUDIT_2026-10-18.md) 为准；没有宣称生产迁移、真实积压恢复或所有外部后端均已验收。

---

## 一、已确认的产品契约

1. 服务名标识逻辑服务，实例 ID 标识部署副本；同一副本重启、地址变化、注册恢复均不改变实例 ID。
2. 新副本首次初始化生成独立实例 ID，并在对外提供服务前持久化；已有身份丢失或损坏不能伪装成普通重启。
3. 注册主键只来自服务名与实例 ID，不能拼入 DataCenterID、MachineID、IP、端口或启动 UUID。编码必须无歧义，不能仅用可碰撞的字符串连接。
4. MachineID/DataCenterID 仅服务于 NewID 生成器的唯一性。可以作为注册元数据观察，但注册、续约、注销、TTL 过期不得分配、修改或释放编号。
5. 自动编号在首次分配后持久化；以后启动恢复原绑定，冲突明确失败，不静默换号。`0` 是现有合法编号，不能用它表示“尚未分配”。
6. 本次启动标识可以变化，但只用于内部区分新旧进程和所有权，不替代稳定实例 ID。
7. 自动恢复只重建当前实例的注册；不重启业务服务、不清理数据库/Redis、不重放业务写请求、不更换身份。
8. 实例 ID 稳定不代表旧进程可以继续操作新注册；需要原子的注册所有权校验。旧进程业务写入隔离另有边界，不能将注册 CAS 或 readiness 当成业务提交的 fencing。
9. 唯一性检查对象是实际 Snowflake WorkerID，而不只是 DataCenterID＋MachineID 配置对；重启后还必须保护生成时间边界。
10. 首版稳定恢复支持范围为经测试的 local、Redis、etcd；Consul 和不具备能力的自定义 Provider 明确拒绝启用，不沿用无保护 Agent 写入宣称已支持。

## 二、源码基线与原因

| 位置 | 当前行为 | 需要改变的契约 |
| --- | --- | --- |
| `pkg/server/router/servicecontext.go:906`、`:2311` | 每次初始化生成 ServiceInstanceID UUID | 加载持久实例身份，独立生成启动标识 |
| 同文件 `:939`、`:2331` | AutoMachineID 每次启动认领，结果只赋给内存配置 | 首次分配后持久化，重启恢复绑定 |
| 同文件 `:1800` | 节点 ID 拼接服务名、DC、MachineID、实例 ID | 注册键只依赖服务名和稳定实例 ID |
| `pkg/server/cluster/membership.go:180` | Heartbeat 失败只记日志 | 检测、恢复、状态反馈和有界重试 |
| `pkg/server/cluster/provider_redis.go:137` | 先 Get 再续期，记录丢失后持续 ErrNodeNotFound | 用本地完整节点描述安全重建注册 |
| `pkg/server/cluster/provider_etcd.go:141` | 续约 lease，不独立确认节点键存在 | 同时确认实际注册可见，重建丢失键/lease |
| `pkg/server/cluster/provider_consul.go:138` | 服务消失后返回 ErrNodeNotFound，注册写入 Agent | 首版 stable 明确拒绝，保留 legacy 限制，不以 KV CAS 假定 Agent 原子性 |
| `pkg/server/api/public/health.go:28` | 返回固定 ok | 保留存活语义，新增独立就绪语义 |
| `pkg/utils/snowflake.go:11` | DC/Machine 十进制拼接为 WorkerID，使用底层默认 6 位 WorkerID | 验证实际 WorkerID、位宽与映射别名，不能只验证配置对 |
| `pkg/server/router/servicecontext.go:1131` | NewID 直接调用内存 worker | 增加有效期与时间预留检查，重启不能重用旧时间范围 |
| `pkg/server/cluster/switcher.go:235` | 切换后关闭旧 Provider | 编号权威独立持有客户端，不被发现切换关闭 |
| `pkg/server/router/serviceresolver.go:183` | 首次 List 后维护 Watch 快照 | 验证恢复后快照收敛；处理失效订阅和空候选 |
| `pkg/persistence/database/nosql/reliable_write_store_config.go` | pending 路径含 DC/MachineID | 保持原路径可访问，不因升级换号遗失 pending |

源码分析证明恢复入口缺失；尚未用故障注入证明消费项目每一次故障的最初诱因。本计划不得将上述分析写成已完成的运行验收。

## 三、实施采用的设计边界

### 3.1 身份持久化

拟新增 `Cluster.InstanceID`、`Cluster.IdentityFile` 和迁移期的 `Cluster.IdentityMode`（`legacy|stable`）。前者支持编排层显式指定稳定 ID；IdentityFile 在 stable 模式下必须是副本独占持久卷中的绝对路径，不再默认写入配置目录。共享配置的多副本必须覆盖为各自独立路径或独立卷，不能依赖 IP、PID、随机临时目录或工作目录猜测身份。

首次发布保留 legacy 默认并登记废弃，只能将显式启用 stable 的部署列入新能力验收；06/07 示例显式启用。默认切为 stable 属于后续明确的破坏性版本迁移，不能仅增加可选字段就悄悄要求所有现有应用写文件。legacy 不获得稳定身份与自愈保证，也不能被描述为完成迁移。

身份文件的拟定结构如下；字段是本计划新增契约，并非当前已实现配置：

```json
{
  "version": 1,
  "service": "orders",
  "instance_id": "orders-a",
  "generator": {
    "assigned": true,
    "namespace": "core",
    "encoding": "legacy-decimal-v1",
    "worker_id": 10,
    "worker_bits": 6,
    "datacenter_id": 1,
    "machine_id": 0
  }
}
```

- 锁定独立的 `identity.json.lock`，整个进程生命周期持锁；不得锁定将被原子替换的 identity.json inode。使用现有依赖或平台标准能力，不为文件锁增加第三方依赖。首版明确支持经测试的 Linux/macOS 本地持久文件系统；不把本地锁外推为 NFS、多主挂载或复制卷的分布式锁保证。
- 身份与生成器时间状态均在启动监听前打开并持锁；初始化失败沿现有 ServiceContext 回滚路径释放锁，不泄漏已创建资源。
- 首次创建采用同目录临时文件、文件同步、原子替换及目录同步；权限 0600。写入失败、格式错误、服务名不一致、配置 ID 与持久 ID 不一致都在监听业务端口前失败。
- 已有持久身份为重启权威。编号落盘后同步到内存配置；不把运行中内存配置整体写回并覆盖人工配置或密钥。
- stable 模式运行时不隐式创建身份。拟新增 `cmd/instance-identity/main.go` 的显式离线初始化/导入入口；首次部署先生成身份文件，普通启动只加载。已有 pending 或远端身份绑定时禁止使用 create 覆盖，改为 import/recover。实例 ID 未显式指定时，离线 create 生成一次随机 ID 并落盘。
- 初始身份文件为 `generator.assigned=false`；首次运行通过编号权威完成幂等分配后原子保存完整绑定。远端分配成功而本地写入失败时，下一次启动按同一稳定 ID 找回原绑定，不占用第二个编号。
- 身份文件与生成器绑定纳入副本卷备份。复制卷不能创建新副本；扩容新副本必须使用新身份和空的独立工作卷。

### 3.1.1 初始化与丢失处理矩阵

| 输入状态 | stable 模式动作 |
| --- | --- |
| 全新独立卷，无身份、无历史绑定、无 pending | 仅离线 create 可创建；普通启动提示初始化命令并失败 |
| 身份存在且合法 | 校验服务、显式 ID、版本和锁，恢复原绑定 |
| 身份缺失但显式 ID 有远端绑定或存在 pending | 普通启动与 create 均失败；从备份或经核对的导入清单恢复 |
| 身份损坏/ID 冲突/编号冲突 | 保留原文件并失败，不生成替代身份 |
| 配置目录只读，独立状态卷可写 | 从显式 IdentityFile 运行，不改配置目录 |
| 状态目录只读/路径未指定/锁不可用 | 监听前失败，不退到临时目录 |
| NewServiceContextWithConfig | 与文件配置走相同验证入口；测试须显式提供 t.TempDir 身份路径 |
| 整卷遗失且无备份 | 不宣称恢复原副本；封禁旧身份，完成退役核对后才可创建新副本 |

离线工具的计划用法如下；工具尚未实现，create/import/retire 均不得启动业务监听或删除业务数据：

```bash
rtk proxy go run ./cmd/instance-identity create --config /etc/core/orders.json --service orders --state-dir /data/orders-a --instance-id orders-a
rtk proxy go run ./cmd/instance-identity import --config /etc/core/orders.json --manifest /secure/orders-a-import.json --state-dir /data/orders-a
```

工具从 --config 显式读取并验证编号权威配置，配置只读且不输出连接凭证；不能使用当前工作目录猜测目标权威。create 仅接受空状态目录并以排他方式创建；import 清单必须含既有编号、实际 WorkerID、时间上界、编号权威世代和 pending 原路径。缺少任何安全来源时 import 失败，不根据目录名猜测历史身份。生产导入必须先停止并隔离旧进程。

### 3.2 MachineID 独立分配

采用“同一生成器命名空间内，最终有效 WorkerID 唯一”的策略，命名空间不包含服务名。拟新增独立根配置 `IDGenerator`，其中 `Namespace` 缺省 `core`，`Provider` 与 `Providers` 显式指定编号权威，复用现有连接配置类型但不引用运行期 DiscoveryProvider 对象。不同命名空间/不同编号权威生成的数字 ID 不承诺互相唯一，跨项目合并 ID 前必须统一权威与编号规划。

这属于本计划提出的安全策略；执行任务 1 时先盘点消费方既有编号，不自动重编号。发现历史冲突则阻止相关消费方迁移，记录处置需求。

- 独立保存 `实例身份 → DC/Machine/实际 WorkerID/编码版本` 与 `实际 WorkerID → 实例身份` 的持久绑定，原子创建；自动分配和显式编号均经过同一冲突校验。配置对不同但 WorkerID 相同仍是冲突。
- 首版保留合法既有 `legacy-decimal-v1` 编码和默认 6 位布局，不重新解释历史数字。必须拒绝 WorkerID 超过 63、编码别名冲突、转换溢出或非法范围。`(DC=1,Machine=10)` 与 `(DC=11,Machine=0)` 均映射到 110，既碰撞又越界，不能准入；`(0,10)` 与 `(1,0)` 都映射到合法范围内的 10，也必须按同一 WorkerID 判冲突。
- 自动分配遍历候选配置后计算最终 WorkerID，只认领有效且未占用的候选；容量按有效 WorkerID 计算，不按 DC×Machine 配置组合数量计算。不能将 DC、Machine 各 0..31 解释成 1024 个安全 worker。
- 已有超范围或冲突映射不能自动修好：停止该消费方迁移，单独制定版本化生成器与历史 ID 兼容方案。不能为凑容量直接加位、截断 WorkerID 或修改旧 pending 的编号。
- 分配数据不挂发现 TTL，不在普通退出时释放，也不通过扫描在线服务推算可用编号。绑定存在则恢复原值，包括编号 0；容量耗尽明确失败。
- 编号权威首版使用独立创建、独立持有的 local/Redis/etcd 客户端；可以指向同一后端，但不共用可被发现 Provider.Close 关闭的客户端，不以隐式引用计数解决生命周期。Consul 编号权威首版明确不支持。
- `IDGenerator.Provider`、连接信息、权威世代、命名空间与分配结果持久绑定。缺少显式编号权威配置时 stable 启动失败，不能由 Cluster.Mode=auto 推导 local 降级；发生连接错误、权限错误或权威丢失一律不自动换权威。
- local 编号权威只承诺单进程测试/开发范围，不宣称跨主机唯一。cluster=off 的 stable 模式仍需有效的本地持久身份和生成器时间保护；外部发现失败不影响已绑定编号，但不能放弃实例所有权边界。
- Provider 切换不得重新分配编号；编号权威和发现权威可以分离，发现切换不能自动切换编号权威。
- 编号权威丢失、回滚或整体重建不能被当成空集重新分配。必须有持久权威世代标记与恢复校验；发现状态丢失可以自愈，编号权威丢失则阻止新分配/新生成器启动，先恢复绑定备份并核对存活实例。
- 首版不自动回收编号；必须同时交付下文的显式退役回收工具和验收，不能只写成运维注意事项。
- 本计划不改变合法既有 Snowflake 位布局、已生成 ID 或 `NewID() uint` 的公共签名；必须新增受控 worker 包装和时间预留状态。所有生产 NewID 入口均受包装保护，不能只改 ServiceContext 而漏掉初始化入口。编号分配不再依赖发现 TTL，因此普通发现断连不触发生成器换号。

### 3.2.1 跨重启的生成时间保护

- 身份与生成器状态合并到 `identity.json.generator`，使用同一生命周期锁与一次原子替换；本地保存编码、WorkerID 和最高预留时间，编号账本保存版本、BaseTime、时间位移与远端上界。此实施简化避免两个文件部分提交；恢复旧卷备份不能低估远端历史范围。
- 按时间窗口预留上界，不为每次 NewID 执行 fsync。预留必须先持久成功再对调用方暴露该窗口内任何 ID；更新采用单调 max/CAS，失败不返回尚未受保护的 ID。
- 包装现有 worker：生成候选后按已验证布局解析实际时间 tick，确认它不超过持久预留上界；底层漂移推进越界时，先扩展上界再返回候选。不能只在调用前看系统时间，因为底层 over-cost 会推进逻辑时间。
- 重启取本地与权威记录中较大的预留上界，等待可信当前时间严格超过该上界后才初始化可用 worker；超过启动等待预算明确失败，不换 WorkerID。正常运行发生超出当前安全窗口的时钟异常时停止返回 ID，不能丢失内存后假设序列可重置。
- 包装的失败语义：`NewID() uint` 无法返回错误，保留签名但明确以稳定、可识别的 panic 拒绝返回无保护 ID，同时请求入口撤销准入、控制器进入终止流程；不得返回 0 或伪成功。梳理请求、初始化和受管理后台任务的调用方，确保错误能被记录并有界退出，不能仅关闭 HTTP readiness。
- 首次迁移的旧生成器没有该状态文件。必须在停止旧进程后，从受控导出/完整持久 ID 清单得到可信时间上界，并确认旧进程不可恢复；无法确定上界的部署保持迁移阻断，不以“等固定几秒”或随机采样代替证明。
- 确定性测试覆盖同 tick 快速重启、跨进程时钟回拨、over-cost 推进、预留写失败、崩溃在预留后/返回前、旧备份恢复。测试注入时钟，不靠真实机器调时或重复跑碰撞概率。

### 3.2.2 编号容量与显式退役

默认 6 位布局最多 64 个不同 WorkerID；具体 DC 与 Machine 范围还会限制可选候选，例如 DC=4 时十进制拼接下 Machine=0..9 有效，Machine=10 已得到 410。容量检查必须在分配前给出有效总量、已绑定、退役隔离与可用数量；不修改现有布局来隐式扩容。

工具拟新增以下离线流程，首版与分配器一起交付：

```bash
rtk proxy go run ./cmd/instance-identity retire --config /etc/core/orders.json --manifest /secure/orders-a-retirement.json --dry-run
rtk proxy go run ./cmd/instance-identity retire --config /etc/core/orders.json --manifest /secure/orders-a-retirement.json --apply
```

清单包含实例身份、权威世代、绑定版本、最高预留时间、停机/不可重启的外部证明、pending/Outbox 处理证据。dry-run 只读输出阻断原因；apply 必须再次 CAS 校验版本，将旧身份永久记为 retired，并将 WorkerID 隔离至可信时间超过已预留上界之后才重新放入可分配池。旧身份文件、旧 BootID 和旧备份不得再次启动；旧业务数据不由工具删除或搬移。

回收可用不意味着旧 worker 可安全恢复。无可靠停机/卷隔离证据、pending 未处理、时间未越界、权威回滚时，工具拒绝 apply。测试至少覆盖分配→退役拒绝→证据齐全退役→安全时间后复用，以及旧实例复活被拒绝。部署清单区分“同一副本重建沿用身份”和“真正新增副本占用新 WorkerID”。

### 3.3 注册所有权与启动世代

稳定注册身份为 `(service, instance_id)`；当前地址、端口、诊断编号和启动标识为属性。每次进程启动生成独立 BootID，并使用 Provider 的原子事务/CAS 取得该实例的运行期所有权。

- 另存运行期 owner 记录与注册记录。注册记录丢失时，只允许当前 BootID 重建；不能让已被新进程替代的旧 BootID 再次认领。
- 新 BootID 接管需要证明旧进程不可继续执行：同一受支持持久卷的排他锁接管、已确认旧进程退出，或编排层完成主机/卷隔离。仅 TTL 失效不构成业务安全接管证明。旧 owner 仍有效时等待有界时间后报冲突，不强制覆盖。
- 网络短断后同一 BootID 可以根据持久世代和原子 owner 校验恢复；新 BootID 不能利用“租约过期”直接抢占一个仍可能运行的旧进程。复制卷或跨主机无隔离证明的故障接管，首版拒绝自动进行。
- owner 使用 Provider 时间/租约，不依赖不同主机时钟相等。保留可识别过时 BootID 的世代信息；全量权威丢失时无法证明所有权，拒绝盲目恢复，进入显式恢复流程。
- 编号权威持久保存每实例 `{epoch, boot_id, retired}`，不挂发现 TTL；活跃注册 lease 另存。首次启动原子创建 epoch=1，同 BootID 只允许续期同 epoch；新 BootID 只有通过受支持的排他卷锁和显式接管条件才 CAS 增加 epoch。注册续期、恢复、注销必须同时校验持久 epoch 与当前 lease owner，旧世代不能借 lease 键过期重建自己。
- 心跳、恢复和注销都校验 owner；旧进程退出不得删除新进程记录。任一权威失去持久 epoch 标记后，受影响的运行进程不能自动把空权威当新集群初始化。
- Redis 使用 Lua 原子校验；etcd 使用事务 revision/lease；local 使用同进程原子所有权。Consul stable 模式首版启动即报不支持，不执行 KV 检查后再写 Agent 的双步骤方案；KV CAS 无法原子约束 AgentServiceRegister/ServiceDeregister。未来支持 Consul 需要独立证明权威读取及发布原子性，并通过暂停旧写入后新实例接管的对抗测试。
- stable 模式的支持矩阵是显式契约：local/Redis/etcd 必须各自通过编号与所有权验收，Consul/旧自定义 Provider 拒绝启用；legacy 模式保持旧行为但不宣称自愈或隔离保证。
- 通过加性能力接口扩展现有 `DiscoveryProvider`，保留其现有方法签名。稳定恢复模式要求新能力，旧消费方自定义 Provider 给出明确迁移错误，不降级成无保护写入。

### 3.3.1 stable 启动顺序与依赖

1. 解析模式、状态路径及独立 IDGenerator 配置，取得 identity.json.lock，读取合法身份。
2. 连接编号权威，核对世代与 retired 状态，恢复/首次分配实际 WorkerID；创建首次绑定时尚不允许对外发 ID。
3. 取得本次 BootID 的运行所有权。与发现节点发布分开，此时不宣称业务端口可达；没有接管证明时不得启动同身份的新生成器。
4. 读回本地/权威最大时间上界，执行重启等待，持久预留新窗口后构造受保护 worker；之后才允许需要 NewID 的业务启动步骤。
5. 完成 EnsureStorage 和业务资源初始化，启动监听，确认 gRPC ready 后发布完整节点；回读注册可见后开放就绪与业务准入。
6. 任一步失败按逆序释放本次 owner、资源、客户端与文件锁；不删除已持久绑定和预留时间。

初始化 RouterInfo 的元数据枚举阶段不得靠包级 worker 生成真实业务主键。依赖该行为的调用方要在任务 3B 中移至受保护 worker 就绪后的服务启动步骤；不能为绕开启动依赖而恢复无保护 ID fallback。

### 3.4 运行期恢复与可观测

```text
starting → registered → suspect → recovering → registered
                         └──────────────→ fenced
任意运行状态 → stopping → stopped
```

- MembershipManager 保存冻结的完整本地节点描述和 owner，不从已丢失的远端记录反向恢复自身身份。
- 每轮操作设独立 context 超时；单 worker 串行恢复，退避加抖动，重试延迟有上限，停止时可取消。重复 Start 不增加 worker。
- 续约成功仍周期读回并确认自身可被发现，覆盖“lease 还在、节点键却被删除”的情况。
- ErrNodeNotFound/失效租约触发受 owner 保护的恢复；临时网络错误继续重试；所有权冲突进入 fenced，不覆盖、不分配新 ID。
- ServiceContext 将 membership 状态反馈到 readiness/Runtime；stopping 先取消恢复再注销，切换 Provider 串行完成，旧恢复任务不能写回旧权威。
- `/api/health` 保留存活语义；拟新增 `/api/ready` 返回就绪状态，不携带内部地址或 owner。外部发现注册失效时返回 HTTP 503；恢复并验证可见后返回 200；cluster=off 不等待外部发现。
- 租约本地有效期从续约请求发起时刻计算，扣除安全余量；不能从收到响应时刻开始加完整 TTL，否则延迟响应会虚增有效期。以单调时间与墙钟经过时间中的较大者判断过期；检测到时钟回拨或休眠恢复无法证明有效性时先重新验证权威，不假定平台单调时钟在休眠期间一直推进。
- 必要监听失败或 fenced 时不接受业务请求。维护本地单调时钟的 owner 有效期，HTTP/gRPC 业务入口、NewID 包装与受管理任务每次发起操作前检查；长暂停恢复后的首个操作必须先检查有效期，不能等待下一次心跳才拒绝。owner 过期撤销准入，新一次确认成功后才恢复；冲突则有界停止。
- 注册 CAS 只保证注册记录不被旧进程覆盖，不保证数据库中的在途提交被撤销。入口校验后已发出的 SQL/MQ 可能仍完成，取消 context 也不能证明回滚。首版不提供“任意网络分区下强行换主且旧写入绝不提交”的承诺；同身份跨主机接管必须先外部隔离旧进程。需要无此证明也自动换主的应用，必须另行在存储提交端实现世代 fencing，不能由 Core readiness 代替。
- 测试必须在旧进程暂停于入口校验前、生成 ID 前、提交前的不同位置触发接管，分别验证本地准入拒绝、时间窗口不重用和外部隔离前拒绝自动接管；不得将仅有注册写入拒绝的测试写成业务 fencing 通过。
- Runtime 明确区分自身注册与下游依赖，不能把所有依赖失败递归传播为全群 liveness 失败。日志只记录稳定事件、服务、实例、状态、错误分类和耗时，不输出凭证/业务载荷。
- Resolver 保留节点权威边界。Watch 恢复、周期对账和空候选重新核对必须有界；不回退到旧硬编码地址，不自动重放结果未知的业务写请求。

## 四、文件分工与实施任务

以下新增文件名为执行目标；实施中如现有同职责文件更适合，直接复用，避免平行实现。每个任务按 RED → 最小实现 → GREEN → 只提交本任务文件推进；本轮不执行这些步骤。

### 任务 0：先冻结实际 WorkerID 编码和兼容准入

修改：`pkg/utils/snowflake.go`；新增 `pkg/utils/snowflake_identity_test.go`；`pkg/server/router/servicecontext.go` 的 worker 构造入口。测试旧编码与 stable 校验分别执行，不将修复当成无版本布局变更。

- [ ] 新增 `TestStableWorkerEncodingRejectsAliasAndOverflow`：以 `(1,10)/(11,0)→110`、`(0,10)/(1,0)→10` 为确定性样例，断言 stable 校验拒绝越界，分配器拒绝有效范围内的别名。
- [ ] 新增 `TestStableWorkerEncodingPreservesValidLegacyIDs`：合法配置沿用旧 WorkerID 与 6 位布局，不能为了使测试通过改成另一种编码；覆盖 uint16 截断前拒绝及范围上界。
- [ ] 运行 `rtk proxy go test ./pkg/utils -run 'StableWorkerEncoding' -count=1` 留存精确 RED。
- [ ] 提取有错误返回的实际 WorkerID 计算/验证入口供 stable 模式与分配器共用；legacy 构造保持兼容并登记其限制，不继续以配置对唯一作为证明。
- [ ] 原命令转绿；盘点 06/07 与消费方实际映射、容量、冲突和超范围项。存在不兼容项时停止该部署的迁移，不自动换号或移动 pending。
- [ ] 单独提交编码验证与测试；任务 3 只能在本任务编码契约确定后开始。

### 任务 1：冻结兼容契约与迁移清单

文件：`pkg/server/config/clusterconfig.go`、`pkg/server/config/serverconfig.go`、新增 `pkg/server/config/instance_identity_test.go`、`pkg/server/config/idgenerator.go`、`pkg/server/config/idgenerator_test.go`；`docs/codex/API_COMPATIBILITY_SURFACE.md`、`docs/codex/CONSUMER_COMPATIBILITY_MATRIX.md`、`docs/codex/DEPRECATION_REGISTER.md`。

- [ ] 新增配置解析用例：legacy/stable 模式、显式绝对身份路径、只读配置配合可写状态卷、assigned=true 且 MachineID=0、未知身份版本拒绝；配置校验与读取身份文件的运行期校验分开。
- [ ] 新增 IDGenerator 独立权威配置用例：stable 模式不继承 Cluster.Mode=auto 降级；Consul 权威不支持；缺少 Provider/连接信息明确失败；local 必须显式声明开发范围。
- [ ] 执行 `rtk proxy go test ./pkg/server/config -run 'Identity|Generator' -count=1`，记录新增用例的精确失败断言。
- [ ] 增加可选配置字段、版本校验与配置契约；不改动旧 JSON 字段名称或静默重解释旧 MachineID。
- [ ] 盘点 Runtime、日志、DTO、事件消费组、03/06/07 示例、本地 pending 路径和自定义 Provider 对旧身份的依赖；给每个迁移项列出旧值、目标值、恢复来源。
- [ ] 默认策略采用命名空间内实际 WorkerID 跨服务唯一，校验完整生成布局；记录消费方冲突，禁止自动纠正生产编号。登记 stable 的行为变化与 legacy 默认保留的限制。
- [ ] 同一命令转绿后，提交配置、测试和兼容说明。

### 任务 2：实现稳定身份文件及生命周期锁

新增：`pkg/server/router/instance_identity.go`、`pkg/server/router/instance_identity_test.go`、`cmd/instance-identity/main.go` 及其 create/import 测试。修改：`pkg/server/router/servicecontext.go`、`pkg/server/router/servicecontext_registry_test.go`。

- [ ] 新增 `TestPersistentInstanceIdentityRestart`：同一身份文件两次进程启动，实例 ID 不变，BootID 不同。
- [ ] 新增表驱动测试覆盖初始化矩阵、并发 create、同卷双开、只读状态目录、损坏文件、服务名不一致、显式 ID 冲突、已有 pending 但身份缺失；全部在接受业务请求前失败且不改旧文件。
- [ ] 新增 `TestIdentityLockSurvivesAtomicReplace`：持有 identity.json.lock 并多次替换身份文件时，第二个进程仍无法取得锁；测试受支持文件系统边界与构造失败后的锁释放。
- [ ] 执行 `rtk proxy go test ./pkg/server/router -run 'PersistentInstanceIdentity|IdentityFile' -count=1` 留存 RED。
- [ ] 实现离线 create/import 的排他持久写入，运行期只加载；stable 的 ServiceInstanceID 改为持久值，BootID 单独生成。所有启动入口与测试显式配置状态卷，隔离测试使用 `t.TempDir()`，不向源码或用户 etc 目录写入。
- [ ] 原命令及 `rtk proxy go test -race ./pkg/server/router -run 'PersistentInstanceIdentity|IdentityFile' -count=1` 转绿，提交此任务文件。

### 任务 3：拆出生成器编号权威，保存首次绑定

新增：`pkg/server/cluster/generator_identity.go`、`pkg/server/cluster/generator_identity_test.go`、首版 `generator_identity_{redis,etcd,local}.go` 和测试；Consul 只增加明确拒绝能力的测试。修改：`pkg/server/cluster/machine_id.go`、`pkg/server/router/servicecontext_auto_machineid_test.go`、`pkg/server/router/servicecontext.go`。

- [ ] 新增用例：并发不同实例实际 WorkerID 不重复；跨服务映射别名拒绝；重启取原编号；0 正确持久化；固定编号冲突失败；按有效候选计算容量耗尽，不按配置对数量计算。
- [ ] 新增分离用例：注销服务或删除发现记录，生成器绑定仍存在；新编号分配不得调用 DiscoveryProvider.Register；发现 Provider 切换不改变编号。
- [ ] 新增丢失权威世代、绑定回滚及本地文件落盘失败用例：不启动 Snowflake、不接受业务请求，不遗留可被错误复用的编号。
- [ ] 执行 `rtk proxy go test ./pkg/server/cluster ./pkg/server/router -run 'GeneratorIdentity|AutoMachineID' -count=1` 留存 RED。
- [ ] 实现原子的双向持久绑定和可恢复的“远端分配成功、本地尚未落盘”流程；保留旧 pending 编号，冲突不得自动换号。实际 worker 的可用初始化还需通过任务 3B 的时间门禁。
- [ ] 新增 `TestGeneratorAuthoritySurvivesDiscoverySwitch`：发现切换关闭旧 Provider 后，编号客户端仍可读取绑定/预留时间；服务最终退出只关闭编号客户端一次。
- [ ] 编号客户端由独立生命周期资源持有，与发现客户端单独构建、单独 Close；失败回滚不关闭其他 ServiceContext 的资源。模拟 Cluster.Mode=auto 后端故障，断言 stable 不切到 local 分配。
- [ ] 本地/模拟测试与真实 Provider 并发测试转绿，提交；真实依赖未运行的 Provider 标注 NOT_RUN，不得宣称具备多节点唯一性。

### 任务 3A：交付编号容量报告和离线退役回收

新增：`pkg/server/cluster/generator_retirement.go`、`generator_retirement_test.go`；修改：`cmd/instance-identity/main.go` 及对应测试。

- [ ] 新增 `TestGeneratorRetirementRequiresProof`，覆盖仍有运行 owner、无外部隔离证明、pending 未处理、预留时间未过、权威世代不匹配、CAS 版本变化时拒绝 apply。
- [ ] 新增 `TestGeneratorRetirementAndSafeReuse`：dry-run 不写；apply 原子保存 retired 身份和时间上界；时间安全后才可由新身份认领；旧身份和旧备份恢复被拒绝。
- [ ] 运行 `rtk proxy go test ./pkg/server/cluster ./cmd/instance-identity -run 'Retirement|Capacity' -count=1` 留存 RED。
- [ ] 实现容量报告、清单验证、dry-run/apply、隔离和安全复用；工具不删除业务文件，不自动扫描在线列表推导可回收编号。
- [ ] 原命令转绿后提交；没有回收工具不得将自动扩容视为可交付。

### 任务 3B：实现时间预留包装与重启门禁

新增：`pkg/server/router/generator_runtime.go`、`generator_runtime_test.go`；修改：`pkg/utils/snowflake.go`、`pkg/server/router/servicecontext.go`、`pkg/server/router/request.go`，以及使用初始化 NewID 的调用方测试。

- [ ] 新增 `TestGeneratorRestartTimeFence`：注入时钟和持久上界，在相同 tick 重启、时钟回拨、旧备份恢复时拒绝提前返回 ID，可信时间超过上界后恢复；不能用随机生成后查重代替此测试。
- [ ] 新增 `TestGeneratorReservationBeforeReturn`：预留写入失败不返回 ID；预留后崩溃重启跳过该窗口；底层 over-cost 推进后解析候选实际 tick，越界扩展失败则不返回。
- [ ] 运行 `rtk proxy go test ./pkg/server/router -run 'GeneratorRestart|GeneratorReservation' -count=1` 留存 RED。
- [ ] 实现本地与权威单调上界、窗口持久化、时钟注入和 worker 包装；NewID 保持 uint 签名但对保护失败拒绝返回并走既定终止语义，补请求/后台调用方的错误观察与退出测试。
- [ ] stable 初始化 NewID 必须绑定本实例受保护的 worker；没有已绑定实例时拒绝使用包级 `(1000,1000)` worker 生成真实 ID。仅 legacy/测试兼容入口可以保留旧行为，并明确不能用于 stable 业务事实。
- [ ] 原命令与 race 测试转绿，核对现有性能基线；不为每个 ID 同步磁盘，不凭有序采样断言全部重启安全。

### 任务 4：替换注册键，补齐 owner 与过时进程隔离

修改：`pkg/server/cluster/node.go`、`errors.go`、`provider_redis.go`、`provider_etcd.go`、`provider_consul.go`、`provider_local.go`、对应测试；`pkg/server/router/servicecontext.go`、`servicecontext_grpc_lifecycle_test.go`。

- [ ] 新增 `TestRegistrationIdentityIndependentOfMachineID`：同一 service/instance 的键不受编号与地址变化影响，不同实例不碰撞。
- [ ] 新增 `TestRegistrationOwnerRejectsOldBoot`：A 失去所有权、B 接管后，A 的续约/恢复/注销均不能覆盖或删除 B。
- [ ] 新增同一实例双启动、注册键单独删除、owner 键单独丢失、完整权威丢失用例，区分同 BootID 安全恢复、新 BootID 有停机证明接管和必须 fenced。
- [ ] 新增 `TestExpiredLeaseDoesNotProveOldProcessStopped`：旧进程暂停超过 TTL，复制卷的新进程没有可靠隔离证明时不得接管；同持久卷排他锁在旧进程真正退出后可按原身份重启。
- [ ] 新增 Consul/旧自定义 Provider stable 启动拒绝用例；不得将 KV CAS 后 Agent 写入作为通过标准。
- [ ] 执行 `rtk proxy go test ./pkg/server/cluster ./pkg/server/router -run 'RegistrationIdentity|RegistrationOwner|OldBoot' -count=1` 留存 RED。
- [ ] 实现首版 local/Redis/etcd 原子所有权能力及 BootID 世代记录，移除注册环节的 MachineID 槽位分配。MachineID 字段仅保留为诊断元数据；Consul 保留 legacy 路径但拒绝 stable 能力。
- [ ] 原命令与对应 race 测试转绿，提交；旧节点键迁移采用停旧进程后切换，不让旧二进制与新身份协议混跑。

### 任务 5：实现 Membership 自愈、就绪状态和 Resolver 收敛

修改：`pkg/server/cluster/membership.go`、`membership_test.go`、`pkg/server/router/servicecontext.go`、`serviceresolver.go`、`serviceresolver_test.go`、`servicecontext_runtime_metrics_test.go`；新增 `pkg/server/api/public/ready.go`、`ready_test.go` 并按现有 Health 注册位置接入。

- [ ] 新增 `TestMembershipRecoversMissingRegistration`：注册消失后使用原完整节点描述恢复，实例 ID、BootID、MachineID 均不变。
- [ ] 新增 `TestMembershipRecoveryCancellation`：恢复阻塞期间 Stop 或 Provider 切换，旧请求被取消且注销后不复活；所有权冲突不会无限重注册。
- [ ] 新增 `TestMembershipChecksVisibleRegistration`：Heartbeat 返回成功但 Get/List 不再可见时仍检测到缺失；etcd lease 存活不等于键存在。
- [ ] 新增 readiness 200→503→200、liveness 保持可用、fenced 禁止业务请求，以及 Resolver 1→0→1 节点收敛用例。
- [ ] 新增 `TestExpiredOwnerBlocksFirstResumedOperation`：阻塞/暂停旧进程直到本地 owner 有效期过期，恢复后的首个 HTTP/gRPC 请求、NewID 和受管理任务在下一次心跳之前就被拒绝。
- [ ] 测试入口校验后提交前暂停的反例，断言没有外部停机证明时拒绝自动同身份接管；记录在途操作可能完成的边界，不宣称任意数据库提交已被 Core 撤销。
- [ ] 执行 `rtk proxy go test ./pkg/server/cluster ./pkg/server/router ./pkg/server/api/public -run 'Membership|Ready|Resolver' -count=1` 留存 RED。
- [ ] 按状态机实现单 worker、每轮超时、有上限退避与抖动、状态快照；让 ServiceContext 负责服务退出和 Provider 切换串行化；复用现有 Runtime 指标入口。
- [ ] 原命令及相关包 race 测试转绿，提交。不能仅断言“Register 被调用”，必须断言权威记录、Resolver 和真实调用恢复。

### 任务 6：真实多进程恢复与 pending 重启验收

修改：`examples/integration/helpers.go`（仅缺少通用进程重启能力时）、新增 `examples/integration/06-shop-microservices-three-process/discovery_recovery_test.go`、`examples/integration/07-shop-order-scale-multi-process/identity_recovery_test.go`；`examples/07-shop-order-scale/bootstrap/config.go`、`deploy/` 下对应挂载配置。

- [ ] 在独立测试发现后端和独立持久卷中启动两个调用角色、两个同名服务副本，记录 PID、实例 ID、BootID、编号、pending 路径、业务事实基线。
- [ ] 用受控连接阻断使发现不可用超过 TTL；禁止重启业务进程。恢复连接后断言注册、Resolver、真实 mTLS gRPC 调用在有界时间内恢复，原 PID 与稳定身份不变。
- [ ] 分别删除仅本轮测试命名空间的注册记录、丢失订阅通知，验证周期对账；不得删除编号绑定或对共享 Redis 执行 FLUSHDB。
- [ ] 相同身份卷重启：实例 ID、编号、WorkerID 和 pending 路径不变，BootID 改变；时间门禁通过后恢复未同步事实和 Outbox，以事实数量/幂等键/投影确认无丢失。
- [ ] 新独立卷扩容：实例 ID 与 WorkerID 独立。复制身份冲突、旧进程暂停恢复、恢复与退出并发均明确拒绝越权；只有可靠停机/卷隔离证明的迁移才允许新 BootID 接管。
- [ ] 验证故障窗口前后采样 NewID，并同时通过实际 WorkerID 别名拒绝、位宽边界、跨重启时钟与预留崩溃的确定性测试；有限样本仅作为补充，不代替唯一性契约。
- [ ] 测试恢复预算初定为后端恢复后 30 秒；按阶段输出实际耗时。若默认退避/超时无法满足预算，修正配置并解释，禁止无限等待或反复重试掩盖失败。
- [ ] 运行下节真实进程命令，保存失败与成功证据；依赖不可用记 NOT_RUN，不得只凭健康接口转绿宣布通过。提交测试与示例配置。

### 任务 7：文档、消费方迁移与发布门禁

修改：`README.md`、`docs/ai/core-skill/multiservice-and-observability.md`、`project-layout.md`、`testing-and-release.md`、`docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md`、`EXTERNAL_INTEGRATION_GUIDE.md`、`ROUTERINFO_RUNTIME_GUIDE.md`、任务 1 的兼容文档、`examples/07-shop-order-scale/README.md` 与 `deploy/README.md`。

- [ ] 说明身份卷/锁文件位置、create/import/retire 命令、只读配置支持、各启动入口、有效 WorkerID 容量、IDGenerator 独立配置/生命周期，以及 local/Redis/etcd 与 Consul 的 stable 支持矩阵；消费方不编写注册恢复循环。
- [ ] 记录 ServiceInstanceID 从“每次启动变化”变成“稳定副本 ID”的行为迁移；已有业务事实中的历史实例 ID 不回填、不重写，BootID 仅新增诊断用途。
- [ ] 明确现有 endpoint 派生的事件消费组首版保持不变，避免身份升级顺带重建消费组并重放历史消息；若要变更须另立消息游标迁移方案。
- [ ] 旧版本升级前记录原编号、实际 WorkerID、布局、可信历史时间上界与 pending 路径；先做别名/越界/容量核对，再停机隔离、导入身份/绑定/时间状态，验证原卷后启用 stable。不能把随机采样当完整历史上界；无法证明的部署保持迁移阻断。
- [ ] 回滚前停止新进程并核对注册归属、生成器绑定、pending；保留新增身份文件和数据。旧二进制缺少新协议，不得在仍有新实例运行时直接滚动回退。
- [ ] 门禁全部通过后产出精确 Core commit 和消费方升级清单；生产发布、消费方改依赖和重启另按用户授权执行，不在写计划阶段进行。

## 五、验证命令与完成判据

实现期间从仓库根目录执行；真实依赖使用本轮独立资源。以下全部尚未执行。

```bash
rtk proxy go test ./pkg/utils ./pkg/server/config ./pkg/server/cluster ./pkg/server/router ./pkg/server/api/public ./cmd/instance-identity -count=1
rtk proxy go test -race ./pkg/server/cluster ./pkg/server/router ./pkg/server/api/public -count=1
rtk proxy go test ./examples/integration/06-shop-microservices-three-process -count=1
rtk proxy go test ./examples/integration/07-shop-order-scale-multi-process -count=1
rtk proxy go test ./internal/compat -count=1
rtk proxy bash ./scripts/test.sh api-compat
rtk proxy bash ./scripts/test.sh config-contract
rtk proxy bash ./scripts/test.sh release-contract
```

新增故障用例须先红后绿；Provider 实测结果按 local/Redis/etcd 分开登记，Consul/旧自定义 Provider 记录 stable 不支持及拒绝测试。只要目标 Provider 没有经过恢复、并发所有权、实际 WorkerID 唯一性与重启时间门禁实测，就不能将它列为已支持。

完成必须同时满足：stable 副本重启身份不变；注册键与编号解耦；最终 WorkerID 编码合法且唯一；编号分配不依赖发现 TTL；时间窗口先持久后使用，重启不重用旧范围；过时进程不能操作新注册；新 BootID 接管不能只依据 TTL；编号客户端不被发现切换关闭；容量/退役可执行；断连超过 TTL 后同 BootID 不重启即可恢复真实调用；readiness/本地准入如实变化；pending/Outbox 不丢失；退出不复活；Consul 不支持边界和兼容迁移文档齐全。

## 六、范围与风险

- 不修改 Vue/React 管理界面，不引入新注册中心，不改业务订单/支付状态机，不改变业务数据主键或自动重放写请求。
- 不自动回收历史编号；显式离线退役必须满足证据和时间条件。不自动合并/移动 pending 目录，不修改历史 ServiceInstanceID 字段。
- 稳定身份需要可靠持久卷；销毁卷是新副本，不能承诺无备份恢复原身份。
- 现有合法布局只有有限的实际 WorkerID 容量；不在本计划中无版本加位。绑定中心整体丢失、无法确定旧时间上界或历史编号越界时，停止迁移/分配，不能用注册自愈掩盖。
- 首版只为经测试的 local/Redis/etcd 提供 stable 能力；Consul 明确不支持。不同 Provider 的成功不能互相推断；本地文件锁不承诺跨主机复制卷安全。
- 提交阶段只纳入本任务文件，保留当前未跟踪的 `web/scaffold-vue/` 和 `docs/codex/SCAFFOLD_VUE_CORE_INTEGRATION_STUDY.md`，不得顺带提交或删除。

## 七、计划自查

- [x] 覆盖已确认的稳定实例 ID、注册键、MachineID 解耦与持久化契约。
- [x] 说明实际 WorkerID 别名/位宽、首次分配、编号 0、重启时间门禁、并发冲突、权威丢失、隔离边界和停止竞态。
- [x] 覆盖 pending 路径、事件消费组、历史 DTO、Provider 切换与回滚。
- [x] 为任务 0、1、2、3、3A、3B、4、5、6、7 列出文件、测试场景、RED/GREEN 命令与验收观察点。
- [x] 区分用户已确认契约与计划提出的配置/命名空间策略；未把方案或测试写成已实现。
- [x] 原只读计划阶段没有执行开发或迁移；用户后续已授权实施，实际本地测试与隔离故障见第九节及实现审计报告。生产迁移和部署仍未执行。


## 八、审计发现与 v2 修订对应

| 审计项 | 修订位置 | 必过验证 |
| --- | --- | --- |
| P1-01 实际 WorkerID 编码别名/位宽 | 3.2、任务 0/3 | 越界拒绝、合法别名冲突拒绝、合法旧布局保持 |
| P1-02 重启时间边界缺失 | 3.2.1、任务 3B/6 | 同 tick、回拨、over-cost、崩溃、旧备份的确定性测试 |
| P1-03 注册隔离不等于业务 fencing | 3.3/3.4、任务 4/5/6 | 恢复后首个操作准入；无停机证明不接管；在途提交边界明确 |
| P1-04 编号权威生命周期不完整 | 3.2、任务 1/3 | 独立客户端；发现切换 Close 不影响编号；无 local 隐式降级 |
| P1-05 Consul KV 与 Agent 非原子 | 3.3、任务 1/4/7 | stable 首版明确拒绝 Consul，不以双步骤校验假装支持 |
| P2-01 永久绑定耗尽容量 | 3.2.2、任务 3A/7 | 实际容量报告、dry-run/apply、安全时间后复用及旧身份拒绝 |
| P2-02 初始化与锁文件边界 | 3.1/3.1.1、任务 1/2 | 初始化矩阵、只读配置、各启动入口、原子替换不绕过锁 |

修订状态为“计划已补齐要求”；上述实现、工具、测试、性能与消费方迁移均为 NOT_RUN。下一次执行从任务 0 开始；不能用本表替代代码与运行验收。


## 九、第二轮审计与实施记录（v3）

- 复审补充 R2-01：仅声明 owner/BootID 不足以阻止 TTL 后旧世代重建；已明确不带 TTL 的 epoch 记录与 CAS 接管协议。
- 复审补充 R2-02：续约返回后重新起算 TTL 会把慢响应当有效新租约；已限定从请求发起时刻计算，并明确休眠/时钟异常重新确认。
- 复审补充 R2-03：原文“本轮只改文档”与用户新授权不一致。历史 NOT_RUN 保留；本节逐项追加真实实施证据，不回写为历史已通过。
- 已创建分支：`feat/stable-instance-discovery-recovery`，原基线 `09f8ae3`；保留继承未跟踪文件。
- 修改前基线：`go test ./pkg/utils ./pkg/server/config ./pkg/server/cluster ./pkg/server/router -count=1` 四包通过；因默认沙箱不能访问现有 Go 缓存，使用已批准的提权测试命令运行。
- 后续任务、外部 Provider 故障、多进程回归与全框架检查尚未完成；不能由基线通过推断实现或整体能力已验收。

- 实施发现并修订 R2-04：仓库 tools/go.mod 是独立工具模块，原计划的工具路径不能被根模块测试。离线工具改为根模块 cmd/instance-identity，避免修改 tools 模块依赖或引入循环依赖。

## 十、实施调整与证据边界

- 已实现身份文件/旁路锁、显式配置、独立 local/Redis/etcd 编号账本、实际 worker 唯一约束、Boot 世代、时间窗口预留与离线 create/import/recover/retire/capacity。身份与生成器状态合并为一次原子文件提交；工具位于根模块 cmd/instance-identity。
- 已实现稳定注册能力、完整节点重建、读回确认、有界超时/退避/抖动、旧接口拒绝越过 owner、就绪探针和业务准入。额外补齐受管理事件 Handler 与 Outbox 的准入；任意应用自行启动的后台数据库任务仍须显式检查准入，框架不冒充提交端 fencing。
- 06 真实三进程覆盖无通知删键、断连超过 TTL、约 1.07 秒恢复真实 mTLS 调用、原卷正常重启和退出不复活。07 新增原生四进程双 order 验收，使用本轮专用 MySQL，验证独立编号、真实下单/幂等及原卷重启；没有把现有 Compose 改成自动初始化 stable，也没有触碰旧卷。
- Local/Redis/etcd 原子 owner、编号权威独立生命周期已实测。Consul 仅回归 legacy；stable 配置拒绝。Redis/NATS 真重启的准备与恢复阶段已实测通过，和内存事件中心测试分开登记。
- 原始全量失败日志保留：缺少默认 Redis 的基线失败、共享 Redis 并行流量污染 MQ 全局计数、07 mesh 配置错误、过时 WebSocket 集成夹具。后两项已修复，Redis 计数已在独占实例复测通过；最终源码另跑整模块和 race，不覆盖原始失败。
- 真实未同步 pending 积压在崩溃时恢复、Linux 容器运行、现有 Compose stable 部署、生产消费方升级及真实外部 Casdoor 测试没有由当前原生进程测试证明。不能将其标成已通过。
- 整体框架检查发现源码基线缺少 .github/workflows/ci.yml、ci-scheduled.yml、consumer-smoke.yml；相关 shell 门禁失败。它们不是本次稳定身份实现引入的问题，记录为框架现有发布门禁缺口，不扩展本轮为 CI 重建任务，也不宣称全部发布门禁通过。
- 任务中的逐阶段提交未执行；最终提交以实际审计/门禁状态为准。原始各任务方框没有整体勾选，避免把未完成的部署/积压/消费方验收写成通过。

最终本轮验收：冻结 Go 源码始末 SHA256 一致，245 包为 125 个有测试包通过、120 个无测试文件包；12 包 race、go vet、候选发布脚本、06/07 真实进程、Redis/NATS 真重启、显式 Redis 权威/L3 缓存、管理前端 256 条测试均通过。56 个默认 skip 保留，显式补跑结果另记。三个 workflow 独立静态门禁仍因基线文件缺失失败；candidate 脚本不覆盖该缺口。当前边界与全部发现以实现审计报告为准，未运行的验收项不勾选为完成。
