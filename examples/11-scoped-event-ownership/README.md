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
go test -race ./examples/integration/11-scoped-event-ownership -count=1 -timeout=3m -v
```

覆盖SQL提交计数、非owner查询、两处理PID、真实用户WS/匿名拒绝/他人隔离、健康旧进程仍运行时换主、恢复快照与退订交错、Gateway恢复租约丢失后等待原生15秒重建引用；不发送客户端重订。硬崩溃、TCP断连/迟到ACK、多主题真实多输入批次、有限积压/取消等更底层证据在Core包测试；SQL锁跨原生租约到期在ownership包真实MySQL测试。

完整合同、shared/W=8保全迁移、回退和NOT_RUN见 [指南](../../docs/codex/SCOPED_EVENT_OWNERSHIP_GUIDE.md)与 [1.4.6验收](../../docs/codex/RELEASE_V1_4_6_2026-10-21.md)。本演示不代替消费方UAT、容量压测或生产Redis HA验收。
