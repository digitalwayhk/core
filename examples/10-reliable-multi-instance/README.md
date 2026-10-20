# 10：可靠事件的多实例分键消费

本例用正式 `ServiceContext.SubscribeEvent` 验证 v1.4.4 的跨实例分担。一个 `event-submit` 发布服务与两个同名 `event-worker` 消费服务运行在三个独立进程；消费者共享同一 subject 和逻辑服务 group，使用 `user:<UserID>` 作为 `ShardKey`。不用应用自建 worker、Redis 锁、独立业务组或 Broadcast。

## 运行

要求 Go 及一个**专用于本演示**的 Redis（实际验收 Redis 8.0.3）。不要使用其他项目的 Broker。源码永久位于本目录，运行配置放各自目录。先在 Core 根目录编译：

```bash
go build -o /tmp/core-demo10 ./examples/10-reliable-multi-instance/main
mkdir -p /tmp/core-demo10-producer /tmp/core-demo10-worker1 /tmp/core-demo10-worker2
```

分别在三个终端启动，示例假定专属 Redis 为 `127.0.0.1:16379`；替换为自己的明确地址，三个进程必须使用同一独占前缀：

```bash
cd /tmp/core-demo10-producer
CORE_DEMO10_ROLE=producer CORE_DEMO10_INDEX=3 CORE_DEMO10_REDIS_ADDR=127.0.0.1:16379 CORE_DEMO10_PREFIX=example10-local /tmp/core-demo10 -p 50100 -grpc 51100 -view 0
```

```bash
cd /tmp/core-demo10-worker1
CORE_DEMO10_ROLE=worker CORE_DEMO10_INDEX=1 CORE_DEMO10_REDIS_ADDR=127.0.0.1:16379 CORE_DEMO10_PREFIX=example10-local /tmp/core-demo10 -p 50200 -grpc 51200 -view 0
```

```bash
cd /tmp/core-demo10-worker2
CORE_DEMO10_ROLE=worker CORE_DEMO10_INDEX=2 CORE_DEMO10_REDIS_ADDR=127.0.0.1:16379 CORE_DEMO10_PREFIX=example10-local /tmp/core-demo10 -p 50300 -grpc 51300 -view 0
```

系统端口为上述 `-p`，业务端口分别为 `50101/50201/50301`。实例编号必须唯一；每个运行目录保存自身配置/身份。内部 insecure gRPC 仅用于本机演示，生产须采用 mTLS 或可信 mesh。

## 操作与观察

在三个服务的业务端口分别访问 `/api/servermanage/testtoken?userid=101&type=0`，提取返回的用户令牌；再为用户 `202` 各取一次。TestToken 仅用于本地开发，不创建持久管理员；每个服务的令牌独立，不混用 Manage/ServerManage 认证域。

用发布服务的用户 101 令牌请求：

```http
POST http://127.0.0.1:50101/api/event-submit/submit
Authorization: Bearer <发布服务用户101令牌>
Content-Type: application/json

{"requestID":"a-1","sequence":1,"delayMillis":3000}
```

立即用用户 202 的发布令牌提交 `{"requestID":"b-1","sequence":1}`。分别用各消费服务对应的用户令牌调用：

```http
POST http://127.0.0.1:50201/api/event-worker/progress
POST http://127.0.0.1:50301/api/event-worker/progress
```

响应含本进程 `pid`、`instanceID`、当前用户的 `receipts` 和 MQ 快照。每实例 `KeyConcurrency=1`、组级 `Prefetch=8`；A 尚在 running 时，另一 PID 应完成 B。再以 101 提交 `{"requestID":"a-2","sequence":2,"failFirst":true}`，验证它在 A1 完成后处理、首次失败重投后完成。`FailFirst` 是**每副本首次**失败，接管到其他副本可能再次首次失败。

Core 保证同 subject/group/key 的 **Broker 接收顺序**，不会根据本例业务 `sequence` 排序，也不提供跨 subject 的全局用户顺序。副本分配随空闲执行能力变化，不承诺轮询或每个用户永久固定同一实例。

## 演示边界

这里没有数据库模型；不建立空壳 `EnsureStorage`，不引入虚假的业务事务。发布接口直接交给正式 MQBridge，成功只表示 Broker 接收，**不表示业务已完成**。业务数据和投递意图需原子提交的项目，应按自己的持久层使用事务 Outbox；本例不替代该机制。

进度保存在本进程内，最多 256 条，仅淘汰已完成记录；重启会丢失进度，不能当持久 Inbox 或跨副本幂等依据。生产必须自行选择同业务事务的持久幂等方案。相同用户/requestID 生成稳定 EventID，但此例不承诺冲突 payload 检测或永久去重。

生命周期策略由本例明确选择：一个必需组 `event-worker`、至少保留 1 秒、硬上限 10000 条、有限回收预算。它不是 Core 通用默认值；容量满会返回背压，不能改 observe、清消息或删组绕过。单 key 的阻塞消息充满整个预取预算时，后方新 key 仍会受容量背压。

停掉一个消费进程，另一个可以处理其他空闲 key。硬杀后的未完成 key 要等待原生租约过期才能接管（默认约两分钟）；租约过期不证明旧业务代码已停止。完整恢复、连接故障和旧 owner ACK fencing 的独立进程证据见 [修复验收](../../docs/codex/SHARED_KEY_RELIABLE_CONSUMPTION_2026-10-20.md)。

## 自动验收

使用独占 Redis 串行运行，不与全局命令计数测试并发：

```bash
CORE_TEST_REDIS_ADDR=127.0.0.1:16379 go test -p 1 -race ./examples/10-reliable-multi-instance/... ./examples/integration/10-reliable-multi-instance -count=1 -v -timeout=4m
```

测试复用标准进程/HTTP/TestToken 工具，启动三个真实服务，断言两个不同 PID 都处理、慢 key 不阻塞另一个 key、同用户顺序、失败重投、匿名拒绝和用户隔离。没有 Redis 时显式跳过，不将 skip 当通过。
