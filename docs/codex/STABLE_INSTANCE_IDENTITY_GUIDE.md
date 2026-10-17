# 稳定实例身份与注册恢复

本能力通过 `Cluster.IdentityMode=stable` 显式启用。未设置或设置 `legacy` 时保留旧启动行为，不能据此声称已获得稳定身份或注册恢复保证。消费方升级和生产迁移需要单独验收。

## 三种身份的职责

| 数据 | 生命周期 | 用途 |
| --- | --- | --- |
| 服务名 + InstanceID | 同一副本跨正常重启保持 | 注册主键；采用无歧义编码，不包含地址、端口或 MachineID |
| MachineID / DataCenterID / 实际 WorkerID | 持久绑定，发现 TTL 不释放 | 本地 NewID；命名空间内跨服务按最终 WorkerID 排他分配 |
| BootID / BootEpoch | 每次启动更新 | 拒绝旧进程续约、覆盖或注销新进程注册 |

身份及生成器状态合并保存在 `Cluster.IdentityFile`，另持有 `.lock` 旁路锁。原子替换 JSON 不释放锁。文件及锁位于副本独占的持久卷，支持 Linux/macOS 本地文件锁；不承诺复制卷、跨主机文件系统或 Windows 锁语义。业务 pending/Outbox 的路径与数据保持原样。

编号账本使用独立客户端和持久键，不挂发现 TTL；关闭或切换发现 Provider 不关闭编号客户端。local 编号权威只适用于显式 `LocalDevelopment=true` 的开发环境。Redis/etcd 可用于外部权威；Consul stable 不支持，legacy Consul 保留。不同编号命名空间或独立权威之间不承诺数字 ID 唯一。

账本也不能自动看到仍在运行的 legacy worker。需要统一数字 ID 空间时，盘点必须包括系统 server 上下文；旧 worker 未完成停机与绑定迁移前，不得把它与 stable 的全局唯一性混为一谈。06/07 的系统与业务示例编号是已分开的合法配置，不代替消费方盘点。

## 新建副本

配置示例中的时长为纳秒，与现行 Go JSON 配置一致。生产 gRPC 使用应用 mTLS，或确有外部 mTLS 的 mesh；下面的 mesh 仅示意已有安全边界。

```json
{
  "Name": "orders",
  "Port": 18083,
  "DataCenterID": 1,
  "MachineID": 0,
  "Cluster": {
    "Mode": "on",
    "Provider": "redis",
    "IdentityMode": "stable",
    "InstanceID": "orders-a",
    "IdentityFile": "/data/orders-a/identity/identity.json",
    "HeartbeatInterval": 1000000000,
    "Claim": {"AutoMachineID": true, "MachineIDMax": 31},
    "Providers": {"Redis": {"Addr": "127.0.0.1:6379", "Prefix": "core:discovery:orders", "TTL": 10000000000}}
  },
  "IDGenerator": {
    "Provider": "redis",
    "Namespace": "orders-project",
    "Epoch": "initial-20261018",
    "Providers": {"Redis": {"Addr": "127.0.0.1:6379", "Prefix": "core:ids"}},
    "ReservationWindow": 1000000000,
    "StartupTimeout": 5000000000
  },
  "Transport": {"Internal": "grpc", "GRPC": {"Security": {"Mode": "mesh"}}}
}
```

编号配置不继承发现配置；需明确配置连接。编号 `Providers.*.TTL` 必须为零。发现 TTL 至少 1 秒，心跳间隔必须小于 TTL 的 80%。默认时间预留窗口 1 秒、重启等待预算 5 秒。

先准备只读配置和新的独占卷，再显式执行离线工具：

```bash
# 仅用于真正全新的编号权威；丢失既有账本时先恢复备份并核对，禁止重新初始化空账本。
rtk proxy go run ./cmd/instance-identity init-authority --config /etc/core/orders.json
rtk proxy go run ./cmd/instance-identity create --config /etc/core/orders.json --service orders --state-dir /data/orders-a/identity --instance-id orders-a
rtk proxy go run ./cmd/instance-identity capacity --config /etc/core/orders.json
```

`--state-dir` 必须与正式配置的 IdentityFile 父目录一致。create 只接受空目录；不覆盖身份，也不在普通服务启动时自动执行。首次运行完成原子编号绑定；以后从身份卷与权威恢复原绑定，包括合法编号 0，冲突不换号。扩容使用新身份和新独占卷；重建同一副本沿用原卷。

首版保留 `legacy-decimal-v1` 的六位 worker、六位序列和固定 BaseTime。十进制拼接存在别名，例如 `(DC=0,Machine=10)` 与 `(DC=1,Machine=0)` 都得到 worker 10，框架按实际 worker 拒绝冲突。超过 63 的 worker 拒绝；自动 MachineID 候选最多 0..31，具体 DC 会进一步缩小容量，不能解释成 1024 个 worker。需要更多容量时须单独设计版本化 ID 布局。

## 运行期恢复与准入

MembershipManager 保存完整本地节点信息，每轮有超时、串行执行，并在失败后有界退避和抖动。节点键删除或过期后，同 Boot 根据持久 owner 原子重建，并读回确认自身可被发现；不从已丢失的节点猜测身份，不分配新 MachineID，不重启业务进程。

Redis Lua、etcd 事务和 local 互斥保护实际节点写入。owner 元数据不挂发现 TTL；旧 Boot 或 legacy 接口不能覆盖、续期、注销 stable 节点。持久 owner 或编号权威丢失时停止准入并进入终止流程，不把空权威当成新集群。普通网络断连会撤销准入，连接恢复且回读成功后恢复。

- `/api/health` 维持存活检查。
- `/api/ready` 检查本进程服务自身准入；未就绪返回 HTTP 503，恢复后返回 200；不递归依赖下游健康。
- HTTP/gRPC 路由执行在业务 Parse 前检查准入；内部身份授权仍优先执行。
- 受管理的 SubscribeEvent 在 Handler 前检查，可靠事件失败不 ACK；Outbox 在加载和发布前检查，未发布记录留待恢复。进程探针保留已清理 stable 服务的终态故障，同名服务成功重建后才清除。
- 本地有效期从确认请求发起时刻起算，扣除 20% 余量，同时考虑单调时间和墙钟经过时间，防止慢响应、休眠或墙钟回拨延长旧确认。
- 后台任务发起业务操作前使用 `ServiceContext.CheckServiceAdmission()`；框架无法拦截应用自行直连数据库的任意后台写入。
- NewID 的时间窗口先在编号权威和本地持久成功，才返回对应 ID；底层逻辑时间推进越界时先扩展窗口。重启等待时间严格超过本地与远端较大的历史上界，旧备份不能降低远端上界。
- `NewID() uint` 签名保留。临时未就绪以可识别 panic 拒绝，但允许恢复后继续；持久化失败、时钟回拨或编号所有权异常停止发号并触发服务停止。不返回 0 或无保护 ID。无服务上下文的 `InitRequest.NewID` 不再提供包级编号 fallback；元数据枚举不能生成真实业务主键。

注册隔离不等于数据库提交 fencing。入口通过后已发出的 SQL/MQ 仍可能完成；同身份跨主机接管必须先外部隔离旧进程。不得仅根据 TTL 过期强制换主，也不能用 readiness 声称在途事务已回滚。

## 历史迁移、崩溃恢复与退役

迁移前记录既有 DC/Machine、最终 WorkerID、编码、可信完整历史时间上界和 pending 原路径；先核对冲突，再停机隔离。无法证明历史上界、已存在越界/别名冲突、或编号账本回滚时，不迁移、不自动换号。普通运行可检测相对于本地身份文件的 BootEpoch 回滚；同时回滚编号权威和整个身份卷无法由本协议自动识别，恢复备份必须有外部审计与世代规划。

`import` 清单示例：

```json
{
  "state": {
    "version": 1, "service": "orders", "instance_id": "orders-a",
    "generator": {
      "assigned": true, "namespace": "orders-project", "authority_epoch": "initial-20261018",
      "encoding": "legacy-decimal-v1", "worker_id": 10,
      "datacenter_id": 1, "machine_id": 0, "reserved_until": 1790000000000
    }
  },
  "stopped_evidence": "旧进程已停止且不可自动重启；附隔离记录",
  "pending_path": "/data/orders-a/original-pending"
}
```

```bash
rtk proxy go run ./cmd/instance-identity import --config /etc/core/orders.json --manifest /secure/import.json --state-dir /data/orders-a/identity
```

import 只接受尚未运行或已释放的绑定，不覆盖现有身份，不搬移或删除 pending。清单的时间上界必须来自受控历史导出，不能用随机采样或固定等待替代。

正常退出会在成员和资源停止后释放 Boot 所有权，保留编号。硬崩溃后，普通启动拒绝抢占未释放 Boot；先保留或恢复原身份文件，确认旧进程/主机/卷已隔离，再提供精确旧世代清单：

关闭超时或任一资源返回未恢复错误时，ShutdownError 保留错误，Boot 不标记为安全释放；同样需要先核对和隔离再 recover，不能把记录了错误日志当成已成功退出。

```json
{
  "key": "从权威绑定读取的实例 key",
  "epoch": "initial-20261018",
  "boot_id": "从身份文件与权威核对的旧 BootID",
  "boot_epoch": 1,
  "reserved_until": 1790000000000,
  "stopped_evidence": "旧进程不可恢复的隔离记录",
  "pending_evidence": "原 pending 与 Outbox 已核对，路径保留"
}
```

```bash
rtk proxy go run ./cmd/instance-identity recover --config /etc/core/orders.json --manifest /secure/recovery.json --dry-run
rtk proxy go run ./cmd/instance-identity recover --config /etc/core/orders.json --manifest /secure/recovery.json --apply
```

recover 持有本地身份锁，精确 CAS 旧 Boot/版本/时间上界；不清空历史上界、不更换编号。证明字段是可信运维的声明，工具不代替实际隔离。缺失身份文件且没有可靠备份时保持恢复阻断，不能通过 create 伪造原身份。

永久退役使用相同 key/epoch/boot_epoch/reserved_until，加上停机和 pending 处理证据；清单不需要 boot_id。必须已释放 Boot 且可信当前时间严格超过预留范围：

```bash
rtk proxy go run ./cmd/instance-identity retire --config /etc/core/orders.json --manifest /secure/retirement.json --dry-run
rtk proxy go run ./cmd/instance-identity retire --config /etc/core/orders.json --manifest /secure/retirement.json --apply
```

旧身份永久标为 retired，才允许后续副本复用安全的 worker；普通退出不回收编号。工具不删除业务事实。回滚前停止新进程，保留身份与数据；不允许旧二进制与 stable 同身份滚动混跑。

## 06/07 演示与验收

两个示例新增显式环境入口，不自动迁移既有演示数据：

```bash
export SHOP_IDENTITY_ROOT=/data/replica-a/identity
export SHOP_GENERATOR_REDIS_ADDR=127.0.0.1:6379
export SHOP_GENERATOR_NAMESPACE=shop-project
export SHOP_GENERATOR_EPOCH=initial-20261018
```

正式路径为 `$SHOP_IDENTITY_ROOT/<service>/identity.json`。先用与 bootstrap 相同的 Name、编号、Namespace、Epoch 和连接生成配置，再执行 init-authority/create。每个 order 副本使用独占 root 和独立实例 ID；07 原 pending 卷不能改名或共享。07 原 Compose 默认保持 legacy，不能仅设置环境变量而遗漏持久身份初始化。

自动化使用新建独立资源，不访问默认端口或共享库：

```bash
CORE_TEST_REDIS_ADDR=127.0.0.1:16389 go test ./examples/integration/06-shop-microservices-three-process -run TestStableThreeProcessDiscoveryRecovery -count=1 -v
CORE_TEST_REDIS_ADDR=127.0.0.1:16389 CORE_TEST_STABLE_MYSQL_ADDR=127.0.0.1:13389 CORE_TEST_STABLE_MYSQL_PASSWORD='<专用测试库密码>' go test ./examples/integration/07-shop-order-scale-multi-process -run TestStableNativeOrderReplicas -count=1 -v
```

06 检查无通知删键、断连超过 TTL、真实 mTLS 调用恢复、身份不变、正常重启及退出不复活。07 检查真实四进程、双订单副本的独立编号、共享 MySQL 订单、重复请求幂等和原卷重启。测试保留失败证据；目录存在不等于制造并恢复了真实 pending 积压，后者须单独验收。

事件消费组目前仍按既有端点规则派生，历史 ServiceInstanceID 字段不回填；身份迁移不顺带重建消费组或重放历史消息。变更事件游标需要独立方案。
