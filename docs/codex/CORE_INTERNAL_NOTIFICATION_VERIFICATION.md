# Core 内部通知验证记录

## 范围与环境

- 基线：`origin/main` / v1.1.0 `3d4822632afd51dfd99c0f26ff4a4365c1dabe28`；发现流修复 `ffff565`，确认设计 `7d29e84`。实现提交以包含本记录的 Git 历史为准；未回退 retention-only。
- 日期：2026-09-07。独立分支 `fix/redis-discovery-events`，没有修改 Bitzoom 工作区。
- 本地真实 Redis `redis:7.4-alpine`，镜像 `sha256:4ab05801a605362b921756ce9dff4893add29c678076fe49a72d8cc3278806c6`，专用 `core-internal-notify-redis`，`127.0.0.1:52954`，AOF 开启。
- 本地真实 NATS `nats:2.12.8-alpine -js`，镜像 `sha256:36c31459ac1dd3166e7b6a56dc48799e3355fc3c2eee66c73bcb6982c096d124`，专用 `core-internal-notify-nats`，`127.0.0.1:52959`。
- 内部通知证据目录 `/tmp/core-internal-notify-gYGZ6c/`；发现流先前证据 `/tmp/core-discovery-events-cAk9t5/`，详见 [发现流验证](REDIS_DISCOVERY_EVENTS_VERIFICATION.md)。这些为本机日志，不是下游线上 UAT。

## RED → GREEN

| 行为失败 | RED 日志 | 最终验证 |
| --- | --- | --- |
| 普通 MQ 路径保留通知历史且副本竞争 | `red.log` | 两 Provider 原生广播、无历史与独立进程扇出通过 |
| Redis 停止读取仍维持健康 | `red-reader.log` | 读取 watchdog 与 idle 心跳 race 通过 |
| 构造结束取消缓存订阅、断线命中旧缓存、漏 key 通知不收敛 | `red-cache.log` | 生命周期、热路径新鲜度、逻辑版本补偿通过 |
| 已登录无订阅 WS 不因漏撤销/代次变化关闭 | `red-idle-session.log` | 真 WS + 真 Redis 权威注入通知状态通过 |
| 检查忽略 context 时 idle WS 超时仍未关闭 | `red-session-watchdog.log` | 独立 watchdog 通过 |
| 显式缓存 bypass 被新接线改为启动 panic | `red-bypass.log` | 恢复 `RouteCache.Redis.OnUnavailable=bypass`，认证不降级 |
| NATS 异步错误无法唤醒空闲接收 | `red-nats-async.log` | 真连接注入回调、原生 pending 溢出 race 通过 |
| 本地处理器卡住导致内部发布无限等待 | `red-local-budget.log` | 整轮 3 秒预算，定向 race 通过 |

首轮 Broker 重启失败 `race-process-restart.log` 的原因是 Docker 随机映射端口在重启后改变，不是隐藏为通过；仅重建本任务两台测试容器为固定端口，保留日志后重跑通过。没有删除其他测试或生产数据。

追加复验时曾与全仓编译/集成并跑，Heartbeat 20137 次的总 60 秒预算到期；未放宽断言、未改变测试预算。停止并跑后原代码原测试独立 race 通过：Heartbeat 23.01 秒，Register/Heartbeat 的 XLEN 均 10038，Deregister 为 10069，完整 Cluster 包 37.979 秒，见 `race-cluster-isolated.log`。这支持资源竞争下的批量测试耗时解释，不是已证明每次心跳满足生产延迟 SLO。

额外裸跑 `go test ./...` 未传示例用的 `SHOP_REDIS_ADDR`，示例 06 连接默认 6379 失败，停止该轮并保留 `full-suite.log`；不把此轮计为通过。重跑使用隔离 Redis 且降低包级并发，避免与容量/race 用例同时争用资源。

带环境重跑 `full-suite-configured.log` 的唯一失败是示例 07 默认地址测试继承外部 `SHOP_REDIS_ADDR`。仅在该默认值测试中显式清空变量，新增覆盖地址断言；运行时不变。定向 race 已通过 `race-example-env.log`，完整复跑另见 `full-suite-final.log`。

## 已运行验证

| 命令/场景 | 结果与证据 |
| --- | --- |
| `SHOP_REDIS_ADDR=127.0.0.1:52954 go test -p 2 ./... -count=1` | PASS，`full-suite-final.log`，完整退出码 0；示例 06 三独立进程发现与远程调用通过。需显式启用的外部 UAT 仍按自身门禁 SKIP，不等于执行了 Bitzoom/生产 UAT |
| `go vet ./internal/controlnotify ./pkg/server/...` | PASS |
| `CORE_TEST_REDIS=1 CORE_TEST_REDIS_ADDR=127.0.0.1:52954 CORE_TEST_NATS_URL=nats://127.0.0.1:52959 go test -race ./internal/controlnotify ./pkg/server/router ./pkg/server/routecache ./pkg/server/authstate ./pkg/server/trans/websocket/melody ./pkg/server/observability ./pkg/server/event ./pkg/server/types -count=1` | PASS，`race-final-affected.log`，8 包；两种真实 Broker、Redis L3 和真实 WS 均参与 |
| `CORE_TEST_RESTART_NOTIFY_BROKERS=1`，上述 Broker 环境，`go test -race ./internal/controlnotify -run 'TestInternalNotificationBrokerRestart\|TestInternalNotificationMultiProcessFanout' -v -count=1` | PASS，`race-final-process-restart.log`；两独立子进程各收 32 条，两 Broker 真重启后旧连接暴露间隙、新连接可用 |
| 两 Provider 各连续 10240 条；1024/10240 时检查 | PASS，`race-capacity-migration.log` 首轮约 Redis 3.84 秒、NATS 8.69 秒；追加精确新通道无持久化检查后包含在最终全包 race。旧历史均维持 1 条，没有自动删除；短测不等于生产 RSS 稳态容量证明 |
| 真 Redis 两缓存 Manager、真 Badger L2、隔离通知 bus 模拟发布方写完事实但未广播 | PASS，`race-notification-authority.log`；L2 断线不命中、恢复与无断线漏通知均收敛 |
| 生产 `NewServiceContextWithConfig` → 真 Redis/NATS 内部广播 → 真 WS | PASS，`race-composition.log`；异常帧使生产通知桥失效，已登录无订阅连接关闭，HTTP 权威校验仍成功。Casdoor 配置为本地测试公钥，不声称真 Casdoor 登录 UAT |
| 恢复途中第二次断线、重登后旧 watchdog 回调 | PASS，`race-generation-boundary.log`，定向 race |
| `CORE_TEST_REDIS_ADDR=127.0.0.1:52954 CORE_TEST_NATS_URL=nats://127.0.0.1:52959 go test -race ./pkg/server/mq ./pkg/server/cluster -count=1` | PASS，`race-mq-cluster.log`；真实业务 Redis/NATS 可靠订阅与生命周期回归，发现流超过上限/裁剪后 Watch 对账 |
| `config-contract`、`api-compat`、`public-api`、`security`、`release-contract`、`check-logging.sh` | 本地串行全部 PASS；公共 API 门禁没有发现本次删除或签名破坏；基线以来列出的加性 API 不代表本次新增 |
| `gofmt`、`git diff --check`、`check-ai-skill.sh`、skill-creator `quick_validate.py docs/ai/core-skill` | PASS；只更新权威分片，13 个正文文件、4 个指针 |

## 审查结论与边界

按代码审查清单在主线程核对：内部/业务分流、失败关闭、HTTP/WS 认证域、恢复与登录代次、长期 context、整轮预算、初始化/关闭、近似发现上限、旧数据保留、公开接口和配置结构。审查发现的 NATS 异步错误唤醒、缓存显式 bypass、本地交付预算问题均先复现再修复。

NOT RUN：Bitzoom Simple/Full-E4、线上 12 天负载与 OOM 归因、生产峰值 RSS/网络缓冲容量、etcd/Consul 真集群重启、真实 Casdoor 服务交互、跨主机网络分区、带生产 ACL 的 NATS 并发管理面变更。未改动的 Local/etcd/Consul 通过代码盘点区分原生状态与消息历史，不把单元测试描述为真实 Broker 测试。

业务消息不自动删 pending、DLQ 或离线组；旧内部 Stream 升级不自动清理。扩展验收及维护窗口见 [统一内部通知标准](CORE_INTERNAL_NOTIFICATION_LIFECYCLE_GUIDE.md)。发布需在最终提交再次通过 release 检查，不能凭本记录自行移动既有 tag。
