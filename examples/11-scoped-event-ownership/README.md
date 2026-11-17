# 11：跨主题持续归属、SQL fence与WS恢复

演示同一逻辑服务的两个处理进程与两个Gateway。用户A/B是**应用提供的稳定归属键**；两个主题共享scope，每个原 `user:<UID>` 顺序键在各自subject/group内保持顺序。Core负责租约、有限批次、恢复与ACK，models负责SQL fence和事务内消费幂等。

这是技术闭环演示：提交API只返回 `broker-accepted`，不代表业务SQL已完成；示例保留原Stream，不启用生产容量/物理回收策略。没有新增业务发布事实，因此不假装演示了事务Outbox。消费日志/计数是演示技术记录，不是Core默认金融模型，去重记录清理策略由具体项目定义。

## 运行前提

需要**独占**Redis、共享MySQL及逻辑服务 `owned-demo` 的mTLS证书。不要连接Bitzoom现场Broker或数据库。MySQL配置文件采用 `oltp.Config` 的JSON字段、0600权限，不能把密码/DSN提交到Git。进程只通过环境变量取得文件位置，启动错误不打印配置内容。

```bash
export CORE_DEMO11_REDIS_ADDR=127.0.0.1:<专属端口>
export CORE_DEMO11_PREFIX=core-demo11-<本轮唯一值>
export CORE_DEMO11_MYSQL_CONFIG_FILE=<0600配置文件绝对路径>
export CORE_DEMO11_DATABASE=<本轮独占共享数据库名>
export CORE_DEMO11_CA_FILE=<测试CA文件>
export CORE_DEMO11_CERT_FILE=<owned-demo服务证书>
export CORE_DEMO11_KEY_FILE=<服务私钥文件>

# 首次/维护操作：必须停旧消费者并确认实际SQL/handler退出；不放到正常启动中。
CORE_DEMO11_ROLE=prepare go run ./examples/11-scoped-event-ownership/main

# 每条在独立终端运行；实例编号不能重复，仅适用于本机演示。
CORE_DEMO11_ROLE=worker CORE_DEMO11_INDEX=1 CORE_DEMO11_MAX_OWNED=1 \
go run ./examples/11-scoped-event-ownership/main -p 22000 -grpc 23000 -view 0
CORE_DEMO11_ROLE=worker CORE_DEMO11_INDEX=2 CORE_DEMO11_MAX_OWNED=2 \
go run ./examples/11-scoped-event-ownership/main -p 22010 -grpc 23010 -view 0
CORE_DEMO11_ROLE=gateway CORE_DEMO11_INDEX=3 \
go run ./examples/11-scoped-event-ownership/main -p 22020 -grpc 23020 -view 0
CORE_DEMO11_ROLE=gateway CORE_DEMO11_INDEX=4 \
go run ./examples/11-scoped-event-ownership/main -p 22030 -grpc 23030 -view 0
```

业务HTTP位于 `-p+1`，系统管理HTTP位于 `-p`。生产水平扩展按skill启用自动MachineID/stable身份和独占身份卷，不复制演示手工编号或本机地址。示例固定A/B目录、原W=8、每批8条、原生lease15秒；不自动平均搬迁已持有键，总归属槽位需覆盖键目录。

## 操作与验收

在本地开发模式通过 `GET /api/servermanage/testtoken?userid=A`（或B）取普通用户token；不会创建管理员。本人Private API：

| 路由 | 行为 |
| --- | --- |
| `POST /api/owned-demo/submit`，`{"subject":0}`或1 | 从req.GetUser取身份，发布原Envelope；返回Broker接受事实 |
| `POST /api/owned-demo/view` | 非owner Gateway通过正式CallServiceOwned精确查询Ready owner及SQL fence；返回count、epoch、实例/PID与引用计数 |
| `/ws`：logon后 `sub /api/owned-demo/view` | 原会话机制、本人分组、可恢复语义引用；原客户端免重订接收新owner的已提交通知 |

内部 `snapshot` 只接受同逻辑服务的可信mTLS调用；外部HTTP及自报SourceService不能调用。用户身份不取正文，WS通知按用户和当前租约过滤；回复不含token或SQL配置。一个Gateway退订不关闭另一个的引用。

真实集成测试自动生成mTLS证书并启动四个**独立进程**：

```bash
CORE_TEST_REDIS_ADDR=<本轮独占Redis> \
CORE_TEST_MYSQL_OWNERSHIP_CONFIG_FILE=<0600 MySQL配置绝对路径> \
go test -race ./examples/integration/11-scoped-event-ownership -count=1 -timeout=600s -v
```

覆盖SQL提交计数、非owner查询、两处理PID、真实用户WS/匿名拒绝/他人隔离、健康旧进程仍运行时换主、恢复快照与退订交错、Gateway恢复租约丢失后等待原生15秒重建引用；不发送客户端重订。硬崩溃、TCP断连/迟到ACK、多主题真实多输入批次、有限积压/取消等更底层证据在Core包测试；SQL锁跨原生租约到期在ownership包真实MySQL测试。

完整合同、shared/W=8保全迁移、回退和NOT_RUN见 [指南](../../docs/codex/SCOPED_EVENT_OWNERSHIP_GUIDE.md)与 [1.4.6验收](../../docs/codex/RELEASE_V1_4_6_2026-10-21.md)。本演示不代替消费方UAT、容量压测或生产Redis HA验收。

## 动态追加key（v1.8.0）

默认仍为固定A/B。显式设置`CORE_DEMO11_DYNAMIC_KEYS=1`时使用`contract.DynamicManifest()`，W=8/U=8、MaxKeys=16、PageSize=2、每条正文上限64KiB；prepare和各进程必须使用同一声明。示例应用在Initialize通过`sc.RegisterOwnedKey(ctx, manifest, "A")`确认启动key；后续合法key由受信应用控制生命周期调用同一API追加，不公开给Gateway或消息正文自动注册。

本示例的SQL行由应用DML在Activate前幂等初始化；DDL仍只在EnsureStorage启动组合根完成。Core不拥有这些演示用户语义。B/C可在处理进程启动后登记，原消息先于登记仍保留并恢复，单实例可持多个key。故障验收必须预留实际接管槽位，注册成功不代表有Ready owner。

真实回归：`CORE_TEST_REDIS_ADDR=... CORE_TEST_MYSQL_OWNERSHIP_CONFIG_FILE=... go test ./examples/integration/11-scoped-event-ownership -v -count=1 -timeout 600s`，含固定、动态、固定→动态保全迁移三组；动态组包含两个处理进程、两个Gateway、真实mTLS/WS/MySQL以及硬杀后原连接恢复。诊断DTO中的RelayReady只说明对等订阅索引已恢复，不代替Ready/SQL提交；异步Notice不承诺持久可靠投递。

完整接入与回退见[动态目录指南](../../docs/codex/OWNERSHIP_DYNAMIC_KEYS_GUIDE.md)。不要把此组件测试当作消费方容量验收。

### 退出验收（v1.8.1）

引用心跳复用既有服务级中继索引和启动期MQ句柄，停止时取消并等待在途刷新，不要求应用自行停框架worker。集成测试在关闭真实子进程后同时核对实际退出状态和DATA RACE日志，业务断言通过不等于退出通过。无新配置；原固定/动态迁移、SQL fence、PEL及WS免重订断言保持。

## PEL分类复用（v1.13.3）

无需新参数：固定和动态Owned reader均在原有限PendingWindow内保留ID分类，不缓存正文；纯Observe继续核对原PEL、未读范围和目录，实际准入前从原Stream重取正文。原Ready、SQL fence、mTLS/WS恢复、成功后ACK与W/U预算不变。分类只由应用稳定Key回调和Go进程内私有提示形成，不能信任Broker附加字段。已知项缺正文检查延后到实际准入，失败仍不执行handler、不ACK。

升级保留原group/PEL/sequence/SQL fence，无需清数据或重新登记scope；混跑旧副本仍有旧扫描成本。真实多进程、固定→动态迁移与双Gateway验证及剩余性能边界见[扫描修复报告](../../docs/codex/RELIABLE_PENDING_SCAN_FIX_2026-11-16.md)。扫描量下降不等于业务容量或所有时延通过。

## SQL任务与业务暂停

本示例的Broker归属默认不变。原MQ只收单、后续由应用SQL任务执行时，可用同一公开OwnershipStore+ownership.StartRuntime且不Bind原MQ；需要完整Observe和应用同事务SQLfence。显式Observation.BlockedDue仅表达一致SQL事实核实的Due阻挡子集，不是把Due清零、伪造进展或令Core跳过前序。Step无可执行应false,nil，持续错误/未知/实际执行超时仍撤销并等待退出。旧本示例NoDueTasks/Due合同不自动改为新分类，详见[通用接入](../../docs/codex/OWNERSHIP_SQL_TASKS_GUIDE.md)及[真实验收](../../docs/codex/OWNERSHIP_BLOCKED_DUE_2026-11-17.md)。

## 归属指标导出补丁

v1.14.1补齐既有Ownership快照的9项固定名称及固定Keys真实目录长度初始化，查询例如`core_component_gauge{component="ownership",name="ownership_directory_keys"}`；counter使用原`core_component_counter`族。单Runtime目录量可能在多个副本重复，不要相加为全局唯一key数。无新增配置或scope/SQL/PEL迁移；缺失不能补0，详见[原件和升级边界](../../docs/codex/OWNERSHIP_METRICS_EXPORT_GAP_2026-11-17.md)。
