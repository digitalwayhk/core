# 07 原生 go-zero 下单对照

此目录独立实现 07 当前容量测试涉及的下单业务，判断 Core 封装、应用方式与底层存储的成本。应用只使用 go-zero REST/zrpc、grpc-go、GORM、Badger、go-redis、JWT 和 decimal；不调用 Core Router、ServiceContext、业务、模型、DTO、ReliableWriteStore、EventBridge 或事务工具。`TestReferenceDoesNotImportCore` 和依赖清单检查约束这一点。

## 实现的业务闭环

1. user 验证用户域 Access Token，从 Token 取得 UserID；用 mTLS RPC 调 supplier。
2. supplier 从同样的 SQLite 商品、供应商表读取有效资料，保留 COUNT 与快照查询。
3. user 为用户及 requestID 保持同一订单 ID，通过 mTLS RPC 调 order。
4. order 校验规则、数量与金额，读取本地及 MySQL 幂等事实；未配置规则时同样不缓存回退默认值。
5. 订单在 Badger `SyncWrites=true` 提交完成后返回 accepted；合批最多128条，存在积压时收集1ms。
6. 待处理达到32条或最长20ms启动同步，每批最多512条；订单事实与 Outbox 在同一个 MySQL 事务提交。失败保留本地记录，成功后删除本地队列。
7. Outbox 每批100条、最多4路发布；同一订单串行，发布失败保留记录，成功项在一个事务确认。无积压通知时100ms检查。
8. user 以同一逻辑消费组、最多8路处理订单事件，进程内 EventID 去重、成功后 ACK；30秒未 ACK 的 PEL 可 reclaim。此处与 07 相同，是无持久用户库的 facade，不能称为持久 Inbox。

派生队列索引缺失数据时，只有匹配的远端订单、用户请求键与创建事件 Outbox 能证明原事实后才清理；不能因索引残留阻断整个队列，也不能未经证明删除现场。pending计数基于实际本地事实，ACK再次校验ID与请求指纹。长窗中发现并修正的参考实现问题、原始失败与恢复证据见对照报告。

历史不可达p记录在接受请求前核对：只有远端订单的身份、金额、商品及地址快照与创建事件Outbox共同证明原事实，才按本地原始版本CAS删除并扣减实际删除数。无证明或版本变化时保留并报错，不周期扫描重算计数。负数depth停止新接单；列表不用缓存depth分配内存或截断物理读取，达到固定安全上限时显式报错，不返回伪完整结果。

## 公平性与边界

- 三个独立进程、相同依赖版本与测试 schema、相同订单输入、JWT 和证书、MySQL open20/idle5、HTTP客户端5秒/RPC2秒、512在途，以及同样的本地背压阈值。
- 原测试夹具在计时前启动 Core，完成建表、商品准备和 Token 签发，再停止所有 Core 应用。原生应用只检查既有表，直接使用底层库；不会在请求中建表，不新增迁移脚本。
- 双方保留 HTTP 熔断/保护和安全访问日志；原生入站使用 zrpc 默认中间件，Core 入站是自管 grpc-go，差异须计入分析。原生 RPC 直接使用业务类型的 `SupplierService.GetProducts`、`OrderService.CreateOrder/GetOrders`，不复刻 Core 的通用 `Payload/Call` 或内部 JSON 信封。业务数据和一致性语义相同，协议及实现方式的成本属于对比对象。
- Badger 数据布局是独立的两键订单/队列实现，不实现 Core 通用版本、墓碑等扩展能力；它的成本差异正是待测对象，不能将整条链路的差异全部归因 HTTP 引擎。
- 发布与消费通道按完整订单键的稳定散列分配，不能直接对 Snowflake ID 的低位取模；低位序列在低并发下会使大量通道闲置。测试覆盖低位全相同的8,000个订单。
- 原生 peer 地址由测试启动器固定分配，不运行 Core 的服务注册、身份账本、自愈、运行图和业务统计任务。单副本负载不验证这些能力；原生实现也不承诺多副本全局幂等。
- 本轮比较下单、同步和创建事件；没有复制支付、取消、Manage、报表、配置事件失效、订单列表缓存和 WebSocket。无这些操作的负载不能用于宣称整个 07 等价。原生事件处理保持相同的解析、去重和 ACK；测试中无活跃 WebSocket 或缓存命中。
- 同等输入是相同目标速率和请求量；若守护停发或发送器丢弃，必须报告实际发送量，不能称实际处理数据量相等。

## 运行

从仓库根目录，显式配置**独占** MySQL 和每轮全新 Redis，再运行：

```bash
CORE_TEST_ORDER_LOAD=1 \
CORE_TEST_LOAD_SECONDS=30 \
CORE_TEST_LOAD_EVIDENCE_DIR=/绝对路径/本轮证据 \
CORE_TEST_REDIS_ADDR=127.0.0.1:独占端口 \
CORE_TEST_STABLE_MYSQL_ADDR=127.0.0.1:独占端口 \
CORE_TEST_STABLE_MYSQL_PASSWORD=测试密码 \
go test ./examples/integration/07-shop-order-scale-multi-process \
  -run '^TestNativeGoZeroOrderWriteLoad$' -count=1 -timeout=15m -v
```

Core 对照仅替换测试名为 `TestStableOrderWriteLoad`。两者使用同一个发送器、500/s×10秒预热、2000/s主档、采样、pending40000/连续三次采样失败守护、360秒排空和最终 requestID/OrderID 逐键核对。长窗将秒数设为600。不要在两轮间复用已有订单消息的 Redis Stream。

基础检查：

```bash
go test -race ./examples/07-shop-order-scale/go-zero-reference/internal/reference -count=1
go list -deps ./examples/07-shop-order-scale/go-zero-reference/main
```

真实事务回滚测试还需显式 `CORE_REFERENCE_TEST_MYSQL=1` 和 `SHOP_ORDER_REMOTE_MYSQL_*` 指向已准备的独占 schema。测试会插入自己的记录，不清理或删除其他记录。

首次烟测7,000个确认请求全部逐键匹配，仅作为闭环证据。正式对照结果见 [对照报告](../../../docs/codex/GO_ZERO_ORDER_COMPARISON_2026-10-18.md)。

修正恢复逻辑后的原生600秒复验中，680649个确认请求全部匹配，停载约128.05秒后各层排空；失败率、发送器丢弃和排空时长仍未达到容量门槛。完整运行与恢复通过不代表2000/s容量通过，版本和原始失败记录均在报告中单列。

后续[v1.10.2与v1.10.3版本对照](../../../docs/codex/GO_ZERO_VERSION_COMPARISON_2026-10-18.md)中，两版长窗都未达容量目标，并出现负数pending；新版还有已在远端汇合的无索引本地残留。当时使用独立modfile，未改变仓库默认依赖。异常产生的现场因果仍为NOT_REPRODUCED，不能把一次成功排空当成稳定性保证。

当前修复分支已独立适配Badger v4.9.6、升级go-zero v1.10.3，并增加负数准入拒绝、有界本地列表及历史无索引事实的安全恢复：只有匹配的远端完整订单和持久Outbox意图均存在，才允许按原字节CAS清理本地残留；无证明或内容冲突时保留并报错。真实SQL提交/本地确认前、发布/标记前的SIGKILL恢复及最终全框架回归通过，见[执行记录](../../../docs/codex/ORDER_QUEUE_EXECUTION_2026-10-19.md)。这些结果不能代替现场异常归因或长窗容量。

原生对照与Core示例都显式使用utf8mb4驱动插值，GORM PrepareStmt仍false，不通过Core封装访问SQL。参数引号、反斜线、Unicode、NUL、二进制、NULL、精确金额及真实写事务回滚另有专项；协议减少与容量通过分别记录，见[本轮性能结果](../../../docs/codex/ORDER_QUEUE_PERFORMANCE_2026-10-19.md)。

原生订单MySQL池与Core示例保持最大20、空闲20，同样接受原有环境变量覆盖；这是减少已观测连接反复开关的应用配置，不修改Core全局默认，也不宣称通用最优池大小。

前文open20/idle5为历史初始对照基线。当前组合的8个独立60秒阶梯及600秒验收另见[最终容量记录](../../../docs/codex/ORDER_QUEUE_FINAL_CAPACITY_2026-10-19.md)；保留所有失败、未知结果和空间守护，不将原生单轮通过写成三轮容量通过。

最终Core和原生各三轮2000/s×600秒目标测试现已全部执行，容量各0/3通过。原生r1/r2因SQL积压探测连续超时停发，r3因本地pending达到40000停发，排空均超5秒；不能据此认定go-zero补丁或Core封装是单一原因。用户facade请求/事件去重集合仍无回收，需要业务明确持久权威与保留期后设计有界方案。
