# Core 内部通知生命周期实施计划

> 执行要求：使用 `superpowers:executing-plans` 顺序实施；按仓库 AGENTS.md 不分派子代理。每步遵循 RED → GREEN，未完成门禁不发布。

**目标：** 内部通知不积累 Broker 历史，漏通知依靠权威对账安全收敛，不改变业务 MQ。

**架构：** 在 `internal/controlnotify` 封装有界原生广播，在 router 组合根注入现有缓存/认证桥接口。缓存和长连接分别拥有权威恢复与保护逻辑；业务 EventBridge/MQ 的发布订阅路径不变。

**技术栈：** Go 1.26.6、go-redis v9.21.0、nats.go v1.40.1、现有 EventBridge/RouteCache/AuthState。

## 文件与职责

- `internal/controlnotify/transport.go`：私有协议命名、容量限制、内部连接接口；不是公共 MQ API。
- `internal/controlnotify/redis.go`：原生订阅、发布、接收和断线检测；不使用 Stream。
- `internal/controlnotify/nats.go`：普通非 queue 订阅、持久化捕获检查、禁用重连缓冲。
- `internal/controlnotify/transport_test.go`：真实 Broker 双连接广播、命名隔离、无持久化和失败测试。
- `pkg/server/router/internal_notification_bridge.go`：只给框架两类 Manager 注入的适配器，健康状态和恢复代次。
- `pkg/server/router/servicecontext.go`：启动装配、失败清理和关闭；不改变业务 MQBridge。
- `pkg/server/routecache/notification_recovery.go` 及测试：持续订阅、失效期间旁路、周期逻辑版本失效与 generation 恢复。
- `pkg/server/trans/websocket/melody/notification_session.go` 及测试：认证通知不可用时保护、存量会话有界检查。
- `pkg/server/authstate/manager.go`、router 认证接线及测试：权威检查和通知健康分离，HTTP 不依赖通知快照。
- 现行 skill、生命周期指南、能力矩阵、CHANGELOG、独立验证记录：交付证据和部署约束。

## 任务 1：原生广播传输

- [x] 增加真实 Redis/NATS 用例：使用现有普通 MQProvider 的两个实例订阅同一内部主题，发布后检查无历史资源；先捕获当前实现确实创建持久化历史的 RED，不用编译失败代替行为失败。
- [x] 引入仅模块内部可见的接口：

```go
type Transport interface {
    Publish(context.Context, []byte) error
    Receive(context.Context) ([]byte, error)
    Close() error
}
```

- [x] 每个连接绑定一个经编码的精确主题；使用 `Open(ctx, config.MQConfig, service, kind)` 构造。`kind` 只接受 `cache`、`identity`；内容上限 64 KiB；NATS pending 上限 256 条/1 MiB；连接/操作超时 3 秒。上下文约束初始化/单次操作，长期连接仅由 Close 管理。
- [x] Redis 直接 `Subscribe/Receive`，不用会静默丢数据的 Channel 快捷封装；显式取消、意外重新订阅和接收错误上报连续性丢失，不自行声称恢复。主题编码 Prefix、DB、service、kind 与版本。
- [x] NATS `SubscribeSync`，禁用自动重连及重连缓冲，检查 subject 不被 JetStream 捕获；权限/查询不确定时失败。管理面禁止后续增加捕获内部主题的 Stream，发送前复查。
- [x] 将同一行为测试切到新内部传输，验证双副本广播、DB/Prefix 隔离、超大消息拒绝、Close、初始化 context 到期不终止连接、断开/慢消费者暴露失败。
- [x] 运行 `go test ./internal/controlnotify -count=1` 和 `go test -race ./internal/controlnotify -count=1`，两个真实 Broker 均必须参与；记录 RED/GREEN 日志。

## 任务 2：组合根适配器及恢复状态

- [x] 为只登记两类内部主题、拒绝未知主题、进程内只分发一次、外部不走 MQBridge 写入编写失败测试。
- [x] 适配器实现已有 `Subscribe`、`SubscribeExternal`、`Publish` 方法；本地订阅委托 ServiceEventBridge，外部只走精确登记的 Transport。收到外部消息走本地 `External:false` 分发，禁止循环转发。
- [x] 生命周期循环采用可取消上下文、单个有界 worker、恢复代次；断线立即失效，重连及权威恢复全部完成才健康。失败后固定有界退避，无离线消息缓冲。
- [x] ServiceContext 根据真实启用的 shared 功能建立适配器；自定义 Provider 不伪装支持。先注册组件再启动后台任务，清理失败初始化，先停止后台任务再关闭 Manager/存储。
- [x] 运行 `go test ./pkg/server/router ./pkg/server/event -count=1` 与定向 race；证明普通应用事件仍使用原持久化 Provider。

## 任务 3：缓存补偿

- [x] 增加构造函数返回后长期订阅仍存活、断线期间 L1/L2 不命中、key 级漏通知、恢复被新一轮断线打断的失败用例。
- [x] 将长期订阅 context 与 3 秒初始化 context 分离；周期 1 秒、每轮 3 秒预算；未在 5 秒新鲜度窗口内对账成功则旁路，Get/Set 热路径检查新鲜度，不能只依赖 ticker 准时执行。
- [x] 每轮权威 generation 同步并推进本地记录版本覆盖 key 级漏通知；使用原物理 key，旧记录由原 TTL/容量回收。恢复代次校验防止旧回调覆盖新断线。Close 等待 worker 退出，不扫描 Broker 历史。
- [x] 用真实 Redis、两个 Manager 验证先写权威后丢通知、恢复竞争、持续更新收敛；运行 routecache 全包测试和 race。

## 任务 4：认证与真实 WebSocket

- [x] 增加通知失效阻止新 Casdoor 长连接、关闭已有连接、HTTP 仍权威校验、其他认证域隔离的失败用例。
- [x] 复用既有权威 `Authorize`/`Current`，不增加快照授权。长连接检查周期 1 秒、轮次预算 3 秒、最多 64 个检查并发中的任务；5 秒无法确认身份新鲜度就关闭受影响连接。队列和快照收集须按批次有界，不为全部用户生成无界临时任务。
- [x] 登录注册与撤销/断线代次关联；旧检查成功不得恢复新一代失效连接；禁止从控制处理器递归等待本分片。
- [x] 真实进程 + WebSocket 覆盖失联、漏广播、Broker 重启、用户隔离；执行 authstate/types/router 定向 race。

## 任务 5：审计补全、迁移和文档

- [x] 再次枚举 Core 发布点及 Provider 追加操作，逐项记录事实与有界/权威/回收归属；不处理通用资源清理。
- [x] 测试保留已有旧通知 Stream，同时新通道不追加；声明协调升级维护窗口、不能混跑、精确资源盘点后显式授权清理；不向业务项目下放日常删除逻辑。
- [x] 更新 `docs/ai/core-skill/multiservice-and-observability.md`、认证/缓存对应分片、`docs/codex/MQ_MESSAGE_LIFECYCLE_GUIDE.md`、能力矩阵和 CHANGELOG；写明瞬时通知是框架内部例外，不能替代业务事件可靠组。
- [x] 记录低基数健康/失败/补偿指标和测试环境、资源数量趋势；未测项标 `NOT RUN`。

## 任务 6：回归、合并与发布

- [x] 运行 gofmt、cluster 全包测试/race、受影响包测试/race、真实 Redis/NATS 业务生命周期回归。
- [x] 执行 `./scripts/test.sh config-contract`、`api-compat`、`public-api`、`security`、`release-contract` 和 `./scripts/check-logging.sh`；不得因为耗时标为通过。
- [x] 自查持久化、认证、并发与兼容变更；失败先增加定向回归测试再修复。
- [ ] 拉取最新 origin/main，核对无冲突/无功能回退后合并，针对最终提交复验发布门禁。
- [ ] 用户已授权验证通过后发布 `v1.1.1`；执行发布检查，再创建 annotated tag、push 与正式发布说明。不移动既有 tag；未闭环不得发布。

## 执行记录

- 设计已确认：`7d29e84`。
- 发现流修复：`ffff565`，既有真实 Redis 证据另见发现流验证文档。
- 实现和逐项证据已归档 `docs/codex/CORE_INTERNAL_NOTIFICATION_VERIFICATION.md`；缓存改用逻辑版本失效，长连接保护集中到 Melody 登录会话层而非每个 Hub 订阅。
- 真实验证与 NOT RUN 边界以验证记录为准。合并/tag/push 必须等待最终发布门禁，不能以本计划替代发布结果。
