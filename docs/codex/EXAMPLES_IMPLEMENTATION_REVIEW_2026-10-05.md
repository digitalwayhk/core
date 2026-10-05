# Core 01–09 示例与框架实现只读审查

日期：2026-10-05。审查者：独立 Codex worker，task_93cdf56c2055 / ctx_919c2425c33c。结论：**REQUEST_CHANGES**。本报告仅提供审查与建议，没有修复项目代码。

## 1. 审查对象、指纹与限制

- 工作目录及 Git 根：`/Users/vincent/Documents/存档文稿/MyCode/digitalway.hk/core-codex`，分支 `main`，HEAD `7cdf8d998d9ac54a9e880736284536d27380838b`，与任务预期一致；未出现放置不匹配或 consumer_fenced。
- 审查对象为 **HEAD + 当前工作区 tracked 修改 + untracked 源码**，不是纯 HEAD。包含 MQ 默认 off、08/09 Catalog 接线、EnsureStorage、RoleCode RBAC、权限树读写和重建的嵌入产物。
- `web/admin` 已提交 gitlink：`18f687c5793aafbfe83c19d75b07ecc1d885e487`；实际检出 HEAD：`b705b5b49a879517e56f973e38c129039a10201c`，其源码工作区 Git 状态干净。
- 当前 dist/build-info.json：frontend_commit=`b705b5b49a879517e56f973e38c129039a10201c`；artifact_sha256=`162360ef5999a3f831339bbf87e4242021d211d1f171530617e978b32c642554`。协调者已独立复算产物哈希一致；本 worker 核对当前指针与 build-info，但没有重新构建。
- `scripts/check-web-dist-sync.sh` 本轮 exit 1，因门禁读取 **已提交** gitlink 18f687c 与当前未提交 build-info b705b5 不同。这是未提交组合尚不满足发布门禁，不能据此认定当前工作区运行的是旧前端；最终必须同一提交纳入 gitlink 与 dist。
- 原始状态完整保存在 `/private/tmp/core-examples-review-20261005/status-before.txt` 与 `admin-status-before.txt`；协调者基线 `/private/tmp/core-examples-review-20261005/baseline.json` 记录 2130 个 tracked/untracked 非 ignored 路径哈希与 missing 哨兵。末次复算结果 `hash-final-check.json` 为 changed=[]。
- 已阅读 AGENTS.md、RTK.md、Orca orchestration 当前技能，以及权威 `docs/ai/core-skill/SKILL.md`；按启动、路由/DTO、models、Manage、可靠写、跨服务/观测、认证/Admin、前端和测试分片审查。涉及结论的关键规范已小段复读，例如 models.md:125、149–151、190。
- 没有使用旧测试结果作为本轮证据；历史记忆仅用于定位既有审查边界，所有技术结论以本轮源码及日志为准。没有联网研究，不依赖外部文献推断当前实现。

### 只读边界与实际副作用

没有修改 tracked/untracked 项目源码，没有格式化、Git 写操作、分支/子模块切换、安装依赖、改配置、停止既有服务或操作现有 DB/Redis/远程数据。Go 日志、cache、overlay、fixture 与新服务运行目录均位于任务专属 `/private/tmp/core-examples-review-20261005/`。

**需明确披露一次边界偏差**：定向 Jest 使用既有 `web/admin/jest.config.ts`，其中 configUmiAlias/createConfig 隐式调用 Umi 生成流程；实际在 2026-10-05 16:29:12 重写/生成 ignored `web/admin/src/.umi-test` 的 58 个文件。Git status 不显示这类文件；基线未记录 ignored 原状态，无法判断是新建还是内容相同的重写。已向协调者 escalation，协调者要求保留该目录，不删除、不恢复、不再运行此配置；据此保留。原 `.umi` 与 `.umi-production` 文件仍为旧时间。不能宣称整个工作区绝对零写入。

第一次 HTTP 探针发生测试代码 nil 类型断言 panic，TestMain 未执行清理，遗留本任务进程 PID 7204；经 command 明确定位其二进制位于本任务目录后，仅对此 PID 发 SIGTERM。最终探针修正后没有 panic，正常清理其服务。没有触碰既有服务。

## 2. 证据层与逐例覆盖矩阵

`SOURCE_TEST` 包含本轮编译、源码契约与单元测试；`API` 只记本轮真实 HTTP/WS 或 HTTP handler 测试；`UAT` 指人类/浏览器真实操作；外部依赖指 Redis、MySQL、真实 Casdoor、mTLS 多进程、Broker/Prometheus 等。PASS 仅代表表中执行范围通过，不能跨层推广。所有浏览器 UAT 都为 NOT_RUN。

| 示例 | 至少一条实际阅读链路 | SOURCE_TEST | API | UAT | 外部依赖 |
|---|---|---|---|---|---|
| 01 最简商城 | main→ShopService→ProductManage→公开商品→Private.AddOrder→Product.FindByID/Order.Insert→GetOrders/删除→用户 WS 过滤 | PASS：本例全包现有测试 | PASS：真实进程 Manage/Public/Private/Auth/WS 集成；首次隔离 cwd 失败后已重跑 | NOT_RUN | NOT_RUN：实际部署/真实 IAM |
| 02 支付 | main→Manage 商品/支付类型→business.OrderService.CreateOrder→PaymentService.CreatePayment→交易 action Clone/Commit→后台确认支付→撤销/退款→DTO/WS | PASS：全包及状态机/事务隔离测试 | PASS：真实进程 Public/Private/Manage | NOT_RUN | NOT_RUN：真实支付提供商，示例为后台确认式演示 |
| 03 继承 | main→ShopManage/BaseDataManage/BusinessManage→Supplier/Product 启停→CreateOrder 双方有效性→支付/退款→只读订单与流水 Manage | PASS：全包继承、支付和模型测试 | PASS：真实进程集成 | NOT_RUN | NOT_RUN：真实提供商/部署 |
| 04 性能 | main→ShopService.Routers 注入 runtime→Start 可靠 store→Manage 成功失效→orderReference SingleFlight→runtime.Save→Group Commit/Badger→ModelList target/SQLite→Flush/查询/支付 | PASS：全包缓存、可靠写、业务测试；启动屏障有源码缺口 | PASS：现有真实进程集成；不等于冷启动故障验证 | NOT_RUN | NOT_RUN：水平部署、外部 L3、实机 SLO |
| 05 Casdoor | main→OnAuth 双域 Claim→请求撤销校验/OnAuthRequest→业务订单/支付→Casdoor 身份事件→IdentityEventService.Record/幂等审计 | PASS：全包认证、业务、身份审计 | PASS：本地 Fake Casdoor 真实进程链路；不是线上 Casdoor PASS | NOT_RUN | NOT_RUN：真实 Casdoor/OAuth/生产配置 |
| 06 微服务 | all-in-one/三进程 main 启动 EnsureStorage→用户地址归属→Private.AddOrder→受限内部 CreateOrder→supplier 快照→订单指纹/事务/Outbox→EventBridge→SupplierOrder+Inbox revision→用户失效/WS | PASS：全例包的现有单元/契约测试 | NOT_RUN：Redis 真实 HTTP/gRPC 多服务流程 | NOT_RUN | NOT_RUN：Redis Streams/发现、mTLS 三进程、故障恢复 |
| 07 水平扩展 | main 共享 MySQL 初始化→user AddOrder 稳定 ID→内部 CreateOrder 规则→LocalOrderWriter/runtime/Badger→bounded Sync→MySQL 幂等+Outbox→买家查询/支付/撤单→用户事件失效/WS、Runtime metrics | **FAIL：新增模型级探针暴露撤销后支付和支付标识覆盖；现有全包测试 PASS** | NOT_RUN：真实 MySQL/Redis API；模型探针不是 HTTP | NOT_RUN | NOT_RUN：MySQL、Redis、自动 MachineID、多副本/mTLS、Prometheus |
| 08 Admin UI | main EnsureStorage→CatalogService→Category/CatalogItem Manage View→主表 Add/Edit→DoAfter SaveChildren→Search LoadChildren→CloneItem→WayPage/WayForm 子表与 import/export 接线 | **FAIL：3 条缺陷 HTTP 探针；另1条自动重建符合演示意图；现有全包/契约 PASS** | **FAIL：父子失败、跨父子行更新、复制搬行；删后自动重建为预期演示行为；原集成 PASS** | NOT_RUN | NOT_RUN：真实浏览器/部署；本例本地 SQLite HTTP 已执行 |
| 09 RBAC | main 复用 08 Catalog→Core ManageStore/角色/主体→JWT Manage role claim→authorizer→Router→权限树读/事务替换→菜单前端树 | PASS：现有 HTTP handler 测试及源码测试；继承 08 已确认缺陷 | PASS：本轮真实进程 menu/schema/Viewer 403/权限变更等现有集成；未单独重复 08 故障探针 | NOT_RUN | NOT_RUN：真实 Casdoor 首管理员/跨进程竞争；本轮 Fake/handler 不能替代它 |

### 框架与前端阅读追踪

- Router/错误：`pkg/server/types/routerinfo.go:443` Exec 前内部调用授权、请求实例获取、Parse、Validation、Do、cache 和回收；`pkg/server/trans/rest/authrequest.go:74` Manage 顺序；PublicError 与嵌套 TypeError 的源码/现有测试已检查。未观察到用普通用户 token 进入 Manage 的本轮成功路径。
- Manage：`service/manage/{operation,add,edit,search}.go` 与 ModelList Save/适配器；尤其核对 DoAfter 失败传播和父子持久化顺序。Add 的错误吞没由真实 HTTP 证实，不能仅靠现有 Manage 包 PASS 排除。
- 持久化：`pkg/persistence/database/oltp/sqlite.go:408,443,473,508,606` 的 Load/Insert/Update/Delete、防御建表、Clone；MySQL 路径与 07 remote store 只读追踪。Core 系统 `smodels.NewManageModelList` 使用 `managestore.Clone()`，初步共享事务疑点已排除。
- 可靠写：`nosql/reliable_write_store.go`、批提交/准入、`model_list_writebehind_target.go`、04/07 runtime 的 Bind/Unbind/typed access；核对本地持久成功不等于远端完成、失败不 ACK、同 key 合并不适合不可合并账务事件。没有执行本轮崩溃/断电恢复或完整 nosql/race gate。
- 本地 Event center / EventBridge：MQ 关闭时应能独立注册、订阅和投递；原审查的 `pkg/server/event` 包测试已通过，但漏掉同进程多 ServiceContext 的实际互通边界。用户指出后补测：同一服务的本地投递和 Outbox 均 PASS；跨服务投递和跨服务 Outbox 均 FAIL，见 P1-05 与第 9 节。不能用缺少 MQ 解释或跳过该内存链路。
- Outbox / MQ 扩展：`pkg/server/event/outbox.go` 的 pending earliest-first、串行/分 key 并发、失败屏障和 MarkPublishedBatch 在框架单元测试中验证；06 业务事实+Outbox 同事务、Supplier Inbox/revision 同事务，07 bounded sync 使用标准入口，属于源码接线结论，不代表无 MQ 的跨服务投递已成功。真实外部 Broker 的 ACK/reclaim/远端消息回收仍为 NOT_RUN，仅限制外部传输层，不限制本地 Event center 测试。
- Admin：未跟随 HTTP 路径猜认证域。核对 RoleCode+Service+Path+Command 精确权限、system_admin/viewer、权限存储错误 fail closed、保存输入注册路由校验；Core 自定义权限逐请求查询。权限树内部命令虽从 View.Commands 隐藏，仍作为 Manage 路由注册和鉴权。
- 前端：阅读 `WayPage/{index,commandForm,RolePermissionModal,permissionTree}` 与 `WayForm/{index,edittable,setProgrammaticValues}`、request 接线和嵌入 dist；本轮 Jest 四组七断言 PASS。没有真实浏览器 DOM/网络 payload 验证，不能写 browser PASS。

## 3. 已证实缺陷（严重度排序）

未发现有足够本轮证据支持的 P0。下列 P1/P2 与可选优化分开；每项证据层明确。

### P1-01：Manage Add 吞掉 DoAfter 错误，08 父子保存部分成功却返回成功

- 触发：新增合法条目，lines 第一行 quantity=1 合法，第二行 quantity=0 非法。
- 代码：`service/manage/add.go:64–74`；`examples/08-admin-manage-ui/api/manage/catalogitemmanage.go:83–99`；`models/catalog_item_persistence.go:54–104,169–174`。
- 失败链路：Add 先 list.Save 提交父表；DoAfter 顺序保存子行，第二行校验失败返回 `(nil,err)`；Add 在内层遮蔽 err，只有 data!=nil 才返回 Hook 结果，最终返回父 Model 和先前 nil err。
- 实际影响：HTTP 200 / success=true，但只保存父记录和第一条子行；客户端误认全成，重试可能遇到编码冲突，无法原子恢复用户输入。框架错误传播缺陷不限于 08，任意 Add DoAfter 返回 nil+error 均可受影响。
- 证据：`probes-http-confirmed.log:1–8`；`TestReviewProbeFailedChildAdd` 明确断言 Should be false。真实 HTTP 后查询持久化得到仅 valid 子行，没有 invalid 子行。
- 建议：Add 独立处理 DoAfter 的 error，不能以 data 非空作为传播条件；08 父表与两类子表由 models 统一事务保存，先完成所有字段/归属校验，任一失败全部回滚。只修错误信封不能解决已经部分提交。

### P1-02：08 编辑 B 可以提交 A 的子行 ID，把 A 的子行迁移到 B

- 触发：建立 A、B 两条资料；取得 A 的 lines[0].id，以该行 modelState=2 填入 B 的 edit body。
- 代码：`examples/08-admin-manage-ui/models/catalog_item_persistence.go:58–59,86–100`；Specs 对应 `119–120,148–162`；`pkg/persistence/database/oltp/sqlite.go:473` Update。
- 失败链路：SaveLines 强制 line.ItemID=B.ID 后，没有查询旧 child 的父归属，直接按已提交 ID Update；现有行被改写为 B 的子行。Specs 结构同构，尚未单独 HTTP 复测。
- 实际影响：合法编辑命令可跨父记录改写/搬移子行；至少是数据完整性破坏。08 当前无租户父级权限，不能额外宣称跨租户漏洞；消费方复制此模式且只按父记录限域时会扩大权限风险。
- 证据：`TestReviewProbeForeignChildUpdate`，confirmed 日志 expected count=1、actual=0，edit success=true，后续查询源 A.lines=null。
- 排除：初轮 `ForeignChildDelete` PASS，不能写为已证实跨父删除；本项只覆盖 Update。确认探针改用 modelState=2。
- 建议：更新/删除均以已加载的 `ID + ItemID` 校验与条件写执行，拒绝归属不符；禁止从客户端 child ID 推断合法性；state=1 必须创建新记录而不能静默 Update。
- 用户反馈补充：前端用 modelState=1/2/3 表达新增/修改/删除的方向正确，当前 WayForm 提交代码已经有该标记。P1-02 探针明确使用 modelState=2，问题在后端修改分支未校验子行原归属；状态指示操作，不证明 child ID 属于当前父记录。三态处理与归属校验必须同时成立，详见第11节。

### P1-03：08 CloneItem 只清父 ID，复制会搬走原始子表

- 触发：将查询得到的带 lines 的完整 A 行作为复制底稿，更换 code/name，执行 cloneitem。
- 代码：`examples/08-admin-manage-ui/api/manage/cloneitem.go:31,48–60`；`models/catalog_item_persistence.go:59,74–78,99`。
- 失败链路：仅把父 ID 置 0/新 ID；原 Lines/Specs ID、哈希、时间仍保留；SaveChildren 重新赋 ItemID 后按旧 ID Update。
- 实际影响：HTTP 200 成功创建新父条目，源条目失去子行。前端常规“选中行复制”即可能触发，无需恶意构造。
- 证据：`TestReviewProbeCloneMovesChildren` 明确 expected 1 / actual 0；confirmed 日志中 clone 返回旧子行 id=4，新 itemID，源 lines=null。
- 建议：复制在 models 中深拷贝，清理父子 ID/哈希/状态/审计时间，子行明确为新增；事务提交且确认源数据保持不变。不要仅在 UI 清 ID 防护。

### P1-04：07 示例业务缺少订单状态和既有支付幂等约束

- 归属：应用示例业务，位于 examples/07-shop-order-scale 的 business/models，不是 pkg/server 或 pkg/persistence 的通用框架缺陷。订单状态、合法迁移、支付业务幂等键及冲突策略必须由应用定义，不应把这些订单规则内置到 Core。
- 触发 A：同一订单先撤销再支付。触发 B：同一订单依次支付 payment-a、payment-b。
- 代码：`examples/07-shop-order-scale/order-service/models/transaction/order_query.go:41–70`；真实调用 `api/public/create_payment.go:47–60`→`business/order_query.go:55`。
- 失败链路：PayRemoteOrderWith 只检查所有权，直接写 paid 与 CurrentPaymentID；CancelRemoteOrderWith 也没有合法状态迁移判定。Order.UpdateWith 的 validate 仅校验标识/金额，不拒绝 cancelled+paid。
- 实际影响：模型函数允许 cancelled+paid 的矛盾状态；重复请求替换支付事实、增加 revision。这里是支付状态演示，并未接真实资金网关，不能宣称已发生资金损失。
- 证据层：**SOURCE_TEST 模型故障探针**，`review_state_test.go` 使用确定性 IDataAction 读写桩运行当前生产函数；`probes-state.log` 显示 nil error、cancelled/paid/revision=3，以及 payment-b 覆盖 payment-a。没有访问 MySQL，没有 HTTP/多副本证明。
- 建议：由07应用示例的business/models集中定义合法迁移与业务幂等。对于该示例当前支付标记语义，建议取消订单拒绝支付、重复同支付标识不产生新事实、不同标识明确冲突；这些是示例业务策略，不是通用框架强制规则。应用也负责查询付款类型有效性，并用框架提供的事务、存储能力落实行锁或版本条件写，把Outbox与实际状态迁移原子绑定。多次支付、分期、退款等其他业务应自行定义相应状态与幂等范围，不能复用此示例建议作为固定业务规则。

### P1-05：无 MQ 的同进程跨服务事件未互通，Outbox 仍确认发布

- 来源：2026-10-05 用户明确设计约束后的总协调补充审查；不是原 worker 初轮已覆盖的结论。Event center 应在进程内独立注册与订阅，MQ 只增加跨进程能力。同一服务内可用并不足以证明同进程多服务模式满足该约束。
- 代码：`pkg/server/router/servicecontext.go:894–899` 每个 ServiceContext 创建独立 Stream；`:1154–1159` 的 UseOutbox 根据是否有外部 Publisher 选择本地模式；`pkg/server/event/servicebridge.go:307–368` 只在该桥自己的 Stream 注册，`:602–630` 也只向该 Stream 本地投递；`pkg/server/event/outbox.go:466` 使用该桥发布。
- 触发：同一测试进程创建 sender、receiver 两个真实 ServiceContext，均配置 `MQ.Mode=off`、`Cluster.Mode=off`，确认 MQManager=nil；receiver 用标准 SubscribeEvent 注册 Subject/EventType，sender 通过本地 ControlDelivery 或标准 UseOutbox 发布同 Subject/EventType。
- 实际结果：本地 Publish 返回 nil，但 receiverCalls=0；Outbox 的 MarkPublished 已被调用，receiverCalls 仍为 0。两个 Stream 指针不同。对照组同一 ServiceContext 的订阅/发布与 Outbox 都收到一次事件。
- 影响：已注册在另一个本地服务上的订阅不被触发；使用该模式的订单投影、入口失效等跨服务事件链可能停留旧状态，发布方却已确认该记录。本次没有运行真实 06/07 SQLite/MySQL 全业务链，不能声称已经验证某个具体投影表内容或线上数据丢失。
- 证明层：当前标准 ServiceContext/EventBridge/Outbox 运行代码 + 受控内存 OutboxStore 桩；不是 Broker 测试，不是 HTTP UAT。四条 overlay 探针为 2 PASS / 2 FAIL，均有行为断言，没有环境或编译失败。
- 建议：建立由进程组合根管理的本地 Event center 与跨服务注册/分发，保持服务自身队列、生命周期与身份边界；MQ 在此基础上扩展跨进程投递。不能通过把默认 MQ 改回 auto/on 或要求启动 Redis 掩盖本地事件缺口。需定义本地控制 Handler 失败、重试/幂等和退订语义；有 MQ 时防止本地与 Broker 回环双投递；不要简单共享 Stream 而忽略可靠消费组与跨服务故障隔离。
- 验收：无 MQ 单服务、同进程多服务、多个可靠 Handler 成功/失败、退订与关闭、Subject/EventType 过滤、Outbox 成功确认/失败保留；启用 MQ 后复测原本地能力和跨进程能力，EventID 幂等防重复。跨进程投递需要 MQ 或等价外部通道，纯内存不承诺跨进程传输。

### P2-02：01–05 启动建表仍不遵守接受请求前屏障

- 触发：首次启动空数据库，或启动时初始化失败。证据为源码生命周期与现行公开启动契约，不是本轮故障注入。
- 代码：01/02/03/05 的 `main/main.go` 直接 NewWebServer/AddIService/Start，没有调用 models.EnsureStorage；01 models/data_action.go 没有暴露该组合根。02 `models/data_action.go:50–65` 在业务事务入口再次 EnsureStorage；03 同型、05 internal/store.RunInTransaction 同型。04 `service.go:63–65` 在异步 Start 才初始化且失败只记录日志返回。
- Core 链路：`pkg/server/run/server.go:169–209` 全部 context IsRun、endInitialization 之后以 goroutine 调用 Start。权威 `docs/ai/core-skill/models.md:149,190` 明确说不能把 EnsureStorage 放异步 Start 或业务 RunInTransaction。
- 实际影响：早期模板把第一次真实 DB 访问/表初始化留给请求；04 初始化失败不能同步阻止监听，也不能提供启动失败给组合根。底层防御建表不是生命周期保证。07 main 已有同步 EnsureStorage，不能误报其建表缺失；07 runtime.Bind 仍晚于监听，是独立 readiness 风险见下一节。
- 核对命令：`rg -n 'EnsureStorage|server.Start' examples/{01-simple-shop,02-shop-payment,03-shop-inheritance,04-shop-performance,05-shop-casdoor-rbac}/main/main.go` 与上列源码行。
- 建议（按用户反馈更新）：优先增加可选服务存储初始化 Hook，由 service.go 的 EnsureStorage 方法转调 models.EnsureStorage，框架在最终配置校验之后、任何服务监听之前同步执行；任一失败终止启动。保持 IService 原必需方法不变，采用加性能力接口避免破坏消费方。该方案尚未实施，目前仍须沿用现行 main 启动前显式调用以满足契约；实施后才能移除重复 main 调用及业务事务入口的正常建表职责。资源绑定 readiness 仍需另行解决，不能把当前异步 Start 当作此 Hook。详细设计与验收见第10节。

## 4. 未证实风险与演示能力缺口（不得当成已经发生的线上缺陷）

| 编号 | 源码线索 / 具体失败设想 | 本轮证据边界 | 建议核实与处理 |
|---|---|---|---|
| R01 高优先级 | 02/03 `business/order.go:DeleteUnpaidOrder` 在事务外 findOwned→状态判断→Delete；04/05 同类。并发 CreatePayment 可在检查后将订单设 pending，随后删除过期读到的订单 | 没有受控并发时间线/真实 DB 故障探针；列风险 | 用隔离 SQLite 两请求 barrier 证明；删除与支付共享事务/锁/版本条件，防止孤儿流水 |
| R02 高优先级 | 07 `business/local_order_writer.go:54–64` 将 FindRemoteOrderByIdempotencyWith 的所有 error 都转 nil，与 README:71 的 fail-closed 承诺不一致；查询失败可能被当“未找到”继续本地接单 | 错误吞没的源码事实明确，但 schema/Begin/Commit 的真实断网路径未执行，不能写“MySQL 不可达必定成功接单” | 区分 typed NotFound 和查询失败；仅 NotFound 放行；隔离 MySQL 注入查询失败/断连/超时，断言不 Save |
| R03 高优先级 | 07 LocalOrderWriter.Accept check local/remote→Save 不是原子 compare-create；同 requestID 并发不同 fingerprint 可通过检查后覆写同本地 key | 没有并发 Badger probe，本轮现有幂等测试 PASS 不等于覆盖该窗口 | local key 级串行/原子比较写；测试两指纹同时接单及恢复后收敛 |
| R04 高优先级 | 07 `remote_order_syncer.go:34` EventID 只含 OrderID+eventType；重复 state/pay revision 变化时 `InsertOutboxIfMissingWith` 可静默保留旧事件，导致最新状态未通知 | 源码链可达；未验证 MySQL Outbox/消费者端实际停留值 | EventID 按真实迁移/revision稳定生成，同一次重试固定，不同状态事实不同 ID；消费者按 revision 防旧消息 |
| R05 前端 | WayForm.index.tsx:280–302 合并顺序为 programmatic→React values→Ant Form→finish；onDataChange:365–376 只更新 React values，旧 Form child array 可覆盖新编辑结果 | SOURCE_REVIEW；未浏览器/React WayForm 回归，现有 setProgrammaticValues 测试不覆盖子表提交 | 组件测试执行两张子表新增/编辑/删除后父提交，核对实际 request body；以当前子编辑状态生成数组 |
| R06 前端/模型 | WayEditTable.defaultAdd 创建非零时间戳临时 id；SaveLines state=1且非零 id 分支直接 Update，跳过 AddValid，是否落库/不落库取决于底层更新语义 | 没有浏览器新增子行业务探针 | UI 临时 key 与持久 ID 分离；后端 state=1明确新记录校验，禁止非零 ID绕过 |
| R07 生命周期 | 04/07 可靠 store/runtime 在异步 IStartService.Start 绑定；REST 可先监听；04失败仅日志返回，07 Stop等待同步 goroutine，底层 IDataAction无context支持可能拉长停止 | 未做慢初始化/远端卡死/Stop deadline实验 | readiness gate在资源绑定完成后才放业务写请求；失败健康状态明确，Stop/同步设独立deadline |
| R08 缓存 | 04事实 cache 无 TTL/容量上限；generation阻止旧加载回填，但在途调用仍返回 loaded；失效丢失或长期 SKU增长可使快照长期存在 | 没有并发失效+下单测试/内存曲线 | 不把TTL替代权威检查；覆盖失效期间加载、加入有界容量与必要freshness约束 |
| R09 权限编辑 | replaceRolePermissions 事务替换没有 revision条件，两管理员同时保存会最后写覆盖；服务发现只收集部分菜单时保存完整选择可能清掉旧服务权限 | 事务原子/空集删除现有测试PASS；未测多人/部分发现 | 显式版本冲突和完整快照校验，未列旧权限的告警继续保留；多服务掉线保存验收 |
| R10 性能/分页 | 07 ListOrders 合并全部本地 pending到已分页remote结果，不再limit；PendingByUser失败被忽略；ListRemoteOrdersWith total只是len(items)，GetOrders当前忽略total | 未做多页/跨副本大量pending API验收；不是已证实分页协议破坏 | 定义accepted窗口与查询partial状态；保证稳定cursor/size，明确total是否真实全量计数 |
| R11 观测 | 07 processedOrderEvents 和 userOrderIDByRequest 都是无界进程内 sync.Map；前者重启丢幂等，后者重启/入口扩容映射漂移 | README已披露后者单入口限制；本轮未做长时运行 | 明确去重保留窗口并有界清理；入口多副本前提升共享幂等或确定性ID，不能以Map当永久Inbox |
| R12 07演示能力缺口 | supplier-service/service.go:18–22 仅注册供应商/商品路由，无 Start 事件订阅、无供应商订单Manage；projection模型存在但未接线。README:21/37宣称消费订单生成引用与订单查询 | 完整supplier组合根源码可核对；外部UAT NOT_RUN | 先决定07范围：补完整供应商消费者/Inbox/revision/查询闭环，或下调README能力承诺；这是可达能力缺口，与已执行业务失败分开 |
| R13 07演示能力缺口 | CreatePayment的PaymentTypeID只在Validation检查非零，Do不调用FindPaymentTypeWith，直接把订单标 paid；未复用06的支付流水与退款状态机 | 源码明确，不把07简化演示直接认定真实支付系统故障 | 定义07只是支付状态模拟还是完整业务；若承诺完整支付，则补启用类型/流水/退款协议与金额验证 |

06 的 SupplierOrder + Inbox + revision 事务与07未接线投影不可混为同一能力。Redis/mTLS/远端故障没有实测，不能因单元桩和同进程调用就确认服务发现安全或集群可靠。

## 5. 可执行优化建议与顺序

以下是优化与改进方向，不是额外计数的缺陷。

| 优先级/收益 | 成本 | 建议 | 可执行验收 |
|---|---|---|---|
| A 极高 | 中 | 建立无 MQ 的同进程跨服务 Event center；MQ 作为扩展传输 | 本地双服务两个失败探针转绿；Handler失败/退订/过滤/生命周期/有MQ去重验收；不能用默认启用Redis代替 |
| A 极高 | 低 | 框架 Add统一传播 Hook错误，不依赖返回data | 加DoAfter nil+PublicError与nil+internal error契约，真实HTTP分别安全422/500；避免只测helper |
| A 极高 | 中 | 08 models拥有一次父子事务与child归属策略；Clone独立深拷贝入口 | 三条缺陷HTTP探针均转绿；自动重建探针的旧期望已撤销，不要求删空；另加Specs归属/失败回滚/源数据不变/新增非零ID拒绝 |
| A 高 | 中 | 07应用business/models定义订单状态与支付幂等；Core不内置订单规则 | cancelled pay拒绝；同payment重复无revision变化；异payment冲突；隔离MySQL并发CAS与Outbox原子验收 |
| A 高 | 低至中 | 服务可选 EnsureStorage Hook + 框架同步启动屏障；资源 readiness 独立验收 | 空库启动、缺目录/权限/存储失败均在接受请求前失败；慢Bind时业务请求不得假装ready |
| B 高 | 中 | 07远端查询错误分类与状态事件identity | 故障注入NotFound/timeout/断连，只有NotFound允许接单；不同revision通知且旧消息不能覆盖 |
| B 高 | 中 | WayForm父提交从当前child编辑状态构造，分离UI临时key | 两张child表新增/编辑/删除后，捕获HTTP body并UI重读+临时DB核对；新增数量0返回安全错误 |
| B 高 | 中 | 06/07真实三进程/多副本闭环证据 | 新建隔离Redis+MySQL后买家/供应商/管理员分别跑UAT；无证书/SAN伪造HTTP计数0、重放、ACK后恢复及runtime状态验收 |
| B 中 | 低 | 08批量加载两类child替代每行查询，保留删除后自动重建演示数据的行为 | 500行查询SQL数由每行多次读降为有界批量读；不牺牲归属过滤或改变演示自动重建语义 |
| B 中 | 中 | 将进程Map去重变成有界、可观测策略 | 长时固定吞吐测试Map内存不线性永久增长；重启与跨入口重试边界公开 |
| C 中 | 低 | 统一查询page/size/partial协议、限定列表上限 | 大量remote+pending多页无重复漏页，非零下游错误不静默变完整成功 |
| C 中 | 低 | 前端发布将gitlink与dist原子纳入同一提交 | check-web-dist-sync PASS、hash复算PASS，再跑真实浏览器；哈希PASS不等于功能PASS |
| C 中 | 低 | Jest配置将生成目录定向任务tmp，测试停止所有timers/listeners | 不生成源码树ignored产物；测试自然退出无open handles；版本化配置需单独授权实现 |
| C 中 | 中 | 补齐观测质量与SLO：pending年龄/同步失败/权威不可用而非只看健康 | 07故障阶段metrics unavailable/partial真实表达；Prometheus scrape及Admin聚合独立验收；跑性能基准再选批次参数 |

建议推进：先补无MQ同进程Event center跨服务能力→Add错误契约→08父子/复制→07状态机与错误分类→启动readiness→前端真实payload回归→06/07隔离多进程故障测试→性能与发布门禁。各步先保留独立行为失败证据再实现，不以改README或把测试skip作为修复闭环。

## 6. 本轮验证命令与证据目录

所有 Go 命令设置：

```sh
export CC=/Applications/Xcode.app/Contents/Developer/Toolchains/XcodeDefault.xctoolchain/usr/bin/clang
export SDKROOT=/Applications/Xcode.app/Contents/Developer/Platforms/MacOSX.platform/Developer/SDKs/MacOSX.sdk
export GOCACHE=/private/tmp/core-examples-review-20261005/go-cache
export GOTMPDIR=/private/tmp/core-examples-review-20261005
export TMPDIR=/private/tmp/core-examples-review-20261005
export GOPROXY=off
```

系统默认Go为1.26.0，本仓库要求1.26.6；最初GOTOOLCHAIN=local被拒绝，属于环境配置失败。改回auto使用本地已有Go1.26.6 toolchain；GOPROXY=off，没有下载/安装依赖。下面命令请通过RTK执行；test-exec.py把测试CWD放任务tmp并复制必要只读fixtures，config包保留pkg/server/config与docs的相对层级。

```sh
rtk proxy go test -p 3 -exec /private/tmp/core-examples-review-20261005/test-exec.py -count=1 -timeout=120s ./examples/01-simple-shop/... ./examples/02-shop-payment/... ./examples/03-shop-inheritance/... ./examples/04-shop-performance/... ./examples/05-shop-casdoor-rbac/... ./examples/06-shop-microservices/... ./examples/07-shop-order-scale/... ./examples/08-admin-manage-ui/... ./examples/09-admin-manage-rbac/...

rtk proxy go test -p 2 -exec /private/tmp/core-examples-review-20261005/test-exec.py -count=1 -timeout=180s ./examples/integration/01-simple-shop ./examples/integration/02-shop-payment ./examples/integration/03-shop-inheritance ./examples/integration/04-shop-performance ./examples/integration/05-shop-casdoor-rbac ./examples/integration/08-admin-manage-ui ./examples/integration/09-admin-manage-rbac

rtk proxy go test -overlay /private/tmp/core-examples-review-20261005/overlay.json -exec /private/tmp/core-examples-review-20261005/test-exec.py -count=1 -run TestReviewProbe -v ./examples/integration/08-admin-manage-ui
rtk proxy go test -overlay /private/tmp/core-examples-review-20261005/state-overlay.json -exec /private/tmp/core-examples-review-20261005/test-exec.py -count=1 -run TestReviewProbe -v ./examples/07-shop-order-scale/order-service/models/transaction
```

源码没有加入probe文件；overlay.json仅把虚拟测试源映射到任务tmp内文件。这是审查复现，不是修复补丁。

| 日志/产物（均在任务目录） | 实际结果 |
|---|---|
| examples-source-tests.log | exit 0，62个有测试包PASS，其余no test files；外部MySQL环境门禁测试有skip，不能按包PASS说MySQL PASS |
| examples-integration.log | 首轮总体exit1，01相对路径manage_test.go缺失；02/03/04/05/08/09均PASS |
| fixture-rerun.log | 01真实进程PASS；Core api/manage与api/public PASS；config还缺相对docs，环境失败 |
| config-final.log | 镜像正确相对docs后config完整包PASS，exit0 |
| core-tests.log | manageauth/event/types/routecache/service/manage PASS；manage/config最初相对fixture失败，不算产品问题 |
| core-targeted-rerun.log | MQ/default/Role/Selection/Binding定向config与Manage PASS |
| probes-http.log | 初版Add行为失败；ForeignChildDelete PASS；Clone因probe nil断言panic，不能当RED |
| probes-http-final.log | 中间版，种子缺失fixture，不能当种子复活证据 |
| probes-http-confirmed.log | 历史运行4条probe断言失败且无panic；前三条对应缺陷，删后自动重建符合用户确认的演示设计，第四条“应保持空”断言期望已撤销，不能计作缺陷RED |
| probes-state.log | 2条模型probe明确断言失败；没有真实MySQL/API |
| frontend-tests.log | 4 suites/7 tests断言PASS，最终命令exit0；Jest输出open handles警告，不能说资源生命周期通过 |
| web-dist-gate.log | exit1，已提交gitlink与当前工作区build-info不一致；待提交发布组合 |
| baseline.json / hash-final-check.json | 2130路径hash/missing哨兵一致；不覆盖ignored生成目录 |

ProcessOptions 默认编译服务子进程时加 `-race`，本轮上述进程集成与HTTP探针的服务端使用race插桩；顶层go test未显式-race，不是完整框架race测试。没有执行全量release-contract、完整nosql suite、外部Docker/Redis/MySQL、浏览器、跨服务证书故障、实际Prometheus或真实远端UAT。

## 7. 完成状态与末尾复核

已逐例读取至少一条真实业务闭环，并将源码、既有测试、新增故障探针、HTTP/WS、Fake Casdoor、浏览器和外部依赖分开记录。原 worker 初轮记录4项P1、2项P2；第9节补测新增P1-05，第13节按用户确认撤销P2-01缺陷定性后，当前有效缺陷总计5项P1、1项P2；未证实风险及07能力缺口单列，不把它们加入已确认缺陷计数。

末尾 root/submodule branch、HEAD、Git状态与本轮启动快照一致；baseline记录的2130路径hash/missing哨兵未改变。报告以外没有修改受版本控制或非ignored源码。ignored `.umi-test` 的58文件生成副作用与证据盲区已完整披露并由协调者明确要求保留，不能将“git status未变”扩大为“整个文件系统零写入”。未修复、未提交、未发布；待协调者复核并由用户授权后另行推进修复。


## 8. 总协调复核与归档

本轮通过 Orca `orchestration` / `orca-cli` 启动新的 Codex worker，实际 provider 为 codex、model 为 gpt-6.1-sol。Run 为 `run_276bc4ee4574`，Task 为 `task_93cdf56c2055`，Dispatch 为 `ctx_919c2425c33c`。协调者收到匹配该 Task、Dispatch、来源 worker 的 `worker_done`，outcome=succeeded 表示审查交付完成，不表示实现通过或缺陷已修复。

协调者直接复读了 Manage Add/Edit、CatalogItem SaveChildren/LoadChildren、CloneItem、07 订单支付/取消及业务调用入口、04 启动资源绑定和 Core 异步 Start 路径；核对最终 HTTP/model 探针日志与隔离 fixture 重跑结果。初版探针 panic、初版删除探针 PASS、隔离 CWD 缺文件失败均未纳入已确认缺陷。浏览器、真实 MySQL/Redis 多进程等仍为 NOT_RUN。

已确认问题索引：

| 编号 | 优先级 | 问题 | 证明层 |
| --- | --- | --- | --- |
| P1-01 | P1 | Manage Add 吞没后置 Hook 错误，父子保存部分提交仍成功 | 当前源码 + 真实 HTTP 故障探针 |
| P1-02 | P1 | 编辑 B 时可用 A 的子行 ID 重新归属子行 | 当前源码 + 真实 HTTP 故障探针，仅 Update |
| P1-03 | P1 | CloneItem 未重置子行持久 ID，复制搬走源子行 | 当前源码 + 真实 HTTP 故障探针 |
| P1-04 | P1 | 07应用示例：撤销后仍可支付，重复支付覆盖支付标识 | 当前 models + IDataAction 桩行为探针，非真实 MySQL/HTTP |
| P1-05 | P1 | 无 MQ 同进程跨服务未投递，Outbox 仍标记发布 | 标准 ServiceContext/EventBridge/Outbox 行为探针；总协调补测 |
| P2-02 | P2 | 01–05 未完整遵守监听前 EnsureStorage 启动契约 | 当前组合根/框架生命周期 + 现行契约，未做冷启动失败注入 |

归档前核对原始基线的 2130 个 tracked/untracked 非 ignored 文件哈希与缺失哨兵均未变化，root/submodule HEAD 和原有 Git 状态未变化。本轮主动新增仓库文件仅本审查文档；测试工具的 ignored `.umi-test` 副作用见第 1 节，不能据此声明整个工作区零写入。该目录原状态不在基线内，未擅自删除或恢复。

补充后的文档完整保留六项有效缺陷及一项已确认预期演示行为、十三项风险/能力缺口、十四条优化建议、逐例覆盖矩阵、命令与执行边界。未执行修复、格式化项目代码、提交、推送或发布。worker 终端在 Orca 中已被用户接管（`user_owned`、`retainedReason=user_takeover`），协调者尊重其保留状态，不关闭用户终端。

### 附录：可重建的故障探针

下列测试源本轮仅通过 Go overlay 注入，没有写进项目测试目录。08 探针借用既有 integration suite；07 探针借用当前 transaction 包模型，使用桩隔离外部数据库。复现应在新的专属临时目录运行，避免使用现有业务数据。临时日志会随环境清理失去，因此保存测试源与第 3 节的实际输出摘要作为文档证据。

#### 08 HTTP 行为探针

以下保留历史探针源。`TestReviewProbeDeletedSeedsResurrect` 的“删除后应保持空”期望已被用户明确撤销；其运行失败不能再作为缺陷证据。其他三条探针仍对应有效缺陷。

```go
package adminui_test

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"net/http"
	"strconv"
	"testing"
)

func reviewRows(t *testing.T, token string) []map[string]interface{} {
	r := suite.RequestJSON(t, http.MethodPost, "/api/manage/catalog/catalogitemmanage/search", token, map[string]int{"page": 1, "size": 500})
	require.True(t, r.Success, r.Body)
	var table struct {
		Rows []map[string]interface{} `json:"rows"`
	}
	require.NoError(t, json.Unmarshal(r.Data, &table))
	return table.Rows
}
func reviewRow(t *testing.T, token, id string) map[string]interface{} {
	for _, r := range reviewRows(t, token) {
		if r["id"] == id {
			return r
		}
	}
	t.Fatalf("row %s missing", id)
	return nil
}
func TestReviewProbeFailedChildAdd(t *testing.T) {
	token := suite.TokenFor(t, "probe", 1)
	c := suite.addCategory(t, token, "probe-partial-category", "探针分类", 0)
	cid, _ := strconv.ParseUint(c.ID, 10, 64)
	r := suite.RequestJSON(t, http.MethodPost, "/api/manage/catalog/catalogitemmanage/add", token, map[string]interface{}{"code": "probe-partial", "name": "父记录", "categoryID": cid, "price": "1", "lines": []map[string]interface{}{{"name": "valid", "quantity": 1, "amount": "1", "lineNo": 1, "modelState": 1}, {"name": "invalid", "quantity": 0, "amount": "1", "lineNo": 2, "modelState": 1}}})
	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(r.Data, &created))
	row := reviewRow(t, token, created.ID)
	t.Logf("HTTP=%d success=%v persisted=%v", r.HTTPStatus, r.Success, row)
	require.False(t, r.Success, "子行校验失败应返回失败，父子应原子回滚")
}
func TestReviewProbeForeignChildUpdate(t *testing.T) {
	token := suite.TokenFor(t, "probe", 1)
	c := suite.addCategory(t, token, "probe-owner-category", "归属分类", 0)
	a := suite.addCatalogItem(t, token, c, "probe-source", "源条目")
	b := suite.addCatalogItem(t, token, c, "probe-target", "目标条目")
	source := reviewRow(t, token, a.ID)
	line := source["lines"].([]interface{})[0].(map[string]interface{})
	target := reviewRow(t, token, b.ID)
	line["modelState"] = 2
	line["name"] = "moved-from-other-parent"
	target["lines"] = []interface{}{line}
	r := suite.RequestJSON(t, http.MethodPost, "/api/manage/catalog/catalogitemmanage/edit", token, target)
	after := reviewRow(t, token, a.ID)
	t.Logf("HTTP=%d success=%v sourceAfter=%v", r.HTTPStatus, r.Success, after)
	rows, _ := after["lines"].([]interface{})
	require.Equal(t, 1, len(rows), "修改 B 不应删除 A 子行")
}
func TestReviewProbeCloneMovesChildren(t *testing.T) {
	token := suite.TokenFor(t, "probe", 1)
	c := suite.addCategory(t, token, "probe-clone-category", "复制分类", 0)
	a := suite.addCatalogItem(t, token, c, "probe-clone-source", "复制源")
	body := reviewRow(t, token, a.ID)
	body["code"] = "probe-clone-destination"
	body["name"] = "复制目标"
	r := suite.RequestJSON(t, http.MethodPost, "/api/manage/catalog/catalogitemmanage/cloneitem", token, body)
	after := reviewRow(t, token, a.ID)
	t.Logf("HTTP=%d success=%v sourceAfter=%v clone=%s", r.HTTPStatus, r.Success, after, r.Body)
	rows, _ := after["lines"].([]interface{})
	require.Equal(t, 1, len(rows), "复制不应搬走原始子行")
}
func TestReviewProbeDeletedSeedsResurrect(t *testing.T) {
	token := suite.TokenFor(t, "probe", 1)
	c := suite.addCategory(t, token, "probe-seed-category", "种子分类", 0)
	_ = suite.addCatalogItem(t, token, c, "sku-phone-case", "种子条目")
	var row map[string]interface{}
	for _, r := range reviewRows(t, token) {
		if r["code"] == "sku-phone-case" {
			row = r
		}
	}
	require.NotNil(t, row)
	for _, v := range row["lines"].([]interface{}) {
		v.(map[string]interface{})["modelState"] = 3
	}
	r := suite.RequestJSON(t, http.MethodPost, "/api/manage/catalog/catalogitemmanage/edit", token, row)
	require.True(t, r.Success, r.Body)
	after := reviewRow(t, token, row["id"].(string))
	t.Logf("sourceAfter=%v", after)
	require.Empty(t, after["lines"], "已删除明细不应被查询重新写入")
}
```

#### 07 models 状态探针

```go
package transaction

import (
	pt "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"testing"
)

type reviewStateAction struct {
	pt.IDataAction
	order   *Order
	updates int
}

func (a *reviewStateAction) Load(_ *pt.SearchItem, result interface{}) error {
	*result.(*[]*Order) = []*Order{a.order}
	return nil
}
func (a *reviewStateAction) Update(v interface{}) error {
	a.order = v.(*Order)
	a.updates++
	return nil
}
func TestReviewProbePayCancelledOrder(t *testing.T) {
	o := NewOrder()
	o.ID = 101
	o.UserID = 1
	o.SupplierID = 2
	o.ProductID = 3
	o.RequestID = "request"
	o.RequestFingerprint = "fingerprint"
	o.Quantity = 1
	o.UnitPrice = decimal.NewFromInt(1)
	o.TotalAmount = decimal.NewFromInt(1)
	a := &reviewStateAction{order: o}
	_, err := CancelRemoteOrderWith(a, 101, 1, "cancel")
	require.NoError(t, err)
	paid, err := PayRemoteOrderWith(a, 101, 1, "payment-a", "pay")
	t.Logf("err=%v orderStatus=%s paymentStatus=%s revision=%d updates=%d", err, paid.OrderStatus, paid.PaymentStatus, paid.OrderRevision, a.updates)
	require.Error(t, err, "撤销订单必须拒绝支付")
}
func TestReviewProbeRepeatedPayment(t *testing.T) {
	o := NewOrder()
	o.ID = 102
	o.UserID = 1
	o.SupplierID = 2
	o.ProductID = 3
	o.RequestID = "request2"
	o.RequestFingerprint = "fingerprint"
	o.Quantity = 1
	o.UnitPrice = decimal.NewFromInt(1)
	o.TotalAmount = decimal.NewFromInt(1)
	a := &reviewStateAction{order: o}
	_, err := PayRemoteOrderWith(a, 102, 1, "payment-a", "pay")
	require.NoError(t, err)
	firstRevision := o.OrderRevision
	paid, err := PayRemoteOrderWith(a, 102, 1, "payment-b", "pay2")
	t.Logf("err=%v paymentID=%s revision=%d firstRevision=%d", err, paid.CurrentPaymentID, paid.OrderRevision, firstRevision)
	require.Equal(t, "payment-a", paid.CurrentPaymentID, "再次支付不能覆盖首次支付标识")
}
```


## 9. 用户反馈后的本地 Event center 补测与纠正

### 为什么原报告写 NOT_RUN

原审查将真实 Broker ACK/reclaim/消息回收归为外部依赖，没有启动专属 Redis/NATS 等 Broker，因此该层没有运行。内存 Event center 并不需要这些外部组件，原轮 `pkg/server/event` 测试也实际通过；但原报告把本地事件机制与 MQ 扩展写在同一条，且未补同进程跨服务验证，容易把外部层的 NOT_RUN 误解成整个事件机制都无法验证。这是审查覆盖与表达遗漏，已修改第 2 节并补 P1-05。

用户明确的设计要求是：Event center 为进程内基础能力，内存中即可注册、订阅和分发；添加 MQ 扩展跨进程能力，不应破坏无 MQ 的本地运行。这里不承诺不同进程能依靠普通内存互通，也不把内存 bus 本身称为持久化 Broker。

### 当前实现与实际结果

| 层级 / 场景 | 本次结果 | 证据 |
| --- | --- | --- |
| MQ 未配置默认 off，Provider 不创建 | PASS | config 三条默认契约及 TestBuildManager_DefaultDoesNotCreateProvider；四条均通过 |
| Event 包现有测试，含本地 Stream、Bridge 与 fake 外部适配器 | PASS | 本次完整 go test ./pkg/server/event，exit 0；fake ACK 不等于真实 Broker ACK |
| MQ=off，同一服务 SubscribeEvent + 本地 ControlDelivery | PASS | TestReviewNoMQSameServiceLocalDelivery：receiverCalls=1 |
| MQ=off，同一服务 UseOutbox | PASS | TestReviewNoMQSameServiceOutbox：marked=true、receiverCalls=1 |
| MQ=off，同进程不同服务的本地投递 | FAIL | TestReviewNoMQCrossServiceLocalDelivery：streamsSame=false、receiverCalls=0，期望1 |
| MQ=off，同进程不同服务的 Outbox 投递 | FAIL | TestReviewNoMQCrossServiceOutbox：marked=true、receiverCalls=0，期望1 |
| 真实 Broker ACK/reclaim/远端回收 | NOT_RUN | 未启动外部 Broker；与上面的本地 PASS/FAIL 独立 |
| 06/07完整业务表投影与多进程/真实数据库验证 | NOT_RUN | 本次只补标准框架运行链路，未创建 Redis/MySQL 外部环境 |

本地运行链路为：ServiceContext 默认创建 EventStream/ServiceEventBridge → SubscribeEvent 注册自身 Stream → UseOutbox 在无外部 Publisher 时 External=false → Bridge.deliver 只投递自身 Stream → 没有本地 Handler 错误则返回成功 → Outbox 确认发布。缺少的是从发布服务向同进程其他服务已注册订阅的内存路由，而不是缺少 MQ 地址。

### 本次命令、证据和探针

沿用第 6 节 Xcode/Go 环境与专属临时 exec 隔离，日志位于 `/private/tmp/core-event-local-review-20261005/`。没有改生产代码或版本化测试源码，没有启动现有服务，也没有运行前端生成流程。

```sh
rtk proxy go test -exec /private/tmp/core-examples-review-20261005/test-exec.py -count=1 -timeout=90s ./pkg/server/event
rtk proxy go test -exec /private/tmp/core-examples-review-20261005/test-exec.py -count=1 -timeout=90s -run 'TestMQConfigApplyDefaults_(EmptyStruct|OldJSON|ExplicitModes)$|TestBuildManager_DefaultDoesNotCreateProvider$' -v ./pkg/server/config ./pkg/server/mq
rtk proxy go test -overlay /private/tmp/core-event-local-review-20261005/overlay.json -exec /private/tmp/core-examples-review-20261005/test-exec.py -count=1 -timeout=90s -run TestReviewNoMQ -v ./pkg/server/router
```

`event-suite.log` 与 `mq-off.log` 为 exit 0；`probes.log` 为 exit 1，四条探针中两个同服务对照 PASS、两个跨服务期望断言 FAIL。未使用 race 插桩，未修复、未提交；以下源仅通过 overlay 注入 router_test 包，复用既有 fakeService fixture。

```go
package router_test

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func reviewLocalContext(t *testing.T, suffix string) *router.ServiceContext {
	name := "event-review-" + t.Name() + suffix
	cfg := config.NewServiceDefaultConfig(name, 0)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	sc := router.NewServiceContextWithConfig(&fakeService{name: name}, cfg)
	require.NotNil(t, sc.ServiceEventBridge)
	require.Nil(t, sc.MQManager)
	t.Cleanup(func() { require.NoError(t, sc.ServiceEventBridge.Close(context.Background())) })
	return sc
}
func reviewSubscribe(t *testing.T, sc *router.ServiceContext, got *atomic.Int32) {
	cancel, err := sc.SubscribeEvent(event.Subscription{Subject: "review.order.created", EventType: "review.order", Reliable: true, Handler: func(context.Context, *event.Envelope) error { got.Add(1); return nil }})
	require.NoError(t, err)
	t.Cleanup(cancel)
}
func reviewPublish(t *testing.T, sc *router.ServiceContext) {
	env := event.NewEnvelope(sc.Service.Name, "review.order", nil)
	env.Subject = "review.order.created"
	require.NoError(t, sc.ServiceEventBridge.Publish(context.Background(), event.PublishRequest{Class: event.ControlDelivery, Subject: env.Subject, Envelope: env}))
}
func TestReviewNoMQSameServiceLocalDelivery(t *testing.T) {
	sc := reviewLocalContext(t, "-own")
	var got atomic.Int32
	reviewSubscribe(t, sc, &got)
	reviewPublish(t, sc)
	require.Equal(t, int32(1), got.Load())
}
func TestReviewNoMQCrossServiceLocalDelivery(t *testing.T) {
	sender := reviewLocalContext(t, "-sender")
	receiver := reviewLocalContext(t, "-receiver")
	var got atomic.Int32
	reviewSubscribe(t, receiver, &got)
	reviewPublish(t, sender)
	t.Logf("MQ=off streamsSame=%v receiverCalls=%d", sender.EventStream == receiver.EventStream, got.Load())
	require.Equal(t, int32(1), got.Load(), "同进程不同服务应通过内存 Event center 收到事件")
}

type reviewLocalOutbox struct {
	mu     sync.Mutex
	marked bool
	done   chan struct{}
}

func (s *reviewLocalOutbox) LoadPending(context.Context, int) ([]event.OutboxMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.marked {
		return nil, nil
	}
	return []event.OutboxMessage{{ID: 1, EventID: "review-event-1", EventType: "review.order", Subject: "review.order.created", Payload: []byte("{}")}}, nil
}
func (s *reviewLocalOutbox) MarkPublished(context.Context, event.OutboxMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.marked {
		s.marked = true
		close(s.done)
	}
	return nil
}
func reviewOutbox(t *testing.T, cross bool) {
	sender := reviewLocalContext(t, "-sender")
	receiver := sender
	if cross {
		receiver = reviewLocalContext(t, "-receiver")
	}
	var got atomic.Int32
	reviewSubscribe(t, receiver, &got)
	store := &reviewLocalOutbox{done: make(chan struct{})}
	require.NoError(t, sender.UseOutbox(store))
	sender.NotifyOutbox()
	select {
	case <-store.done:
	case <-time.After(2 * time.Second):
		t.Fatal("Outbox 未完成发布确认")
	}
	t.Logf("MQ=off cross=%v marked=true receiverCalls=%d", cross, got.Load())
	require.Equal(t, int32(1), got.Load(), "Outbox 本地发布应送达已注册的订阅者")
}
func TestReviewNoMQSameServiceOutbox(t *testing.T)  { reviewOutbox(t, false) }
func TestReviewNoMQCrossServiceOutbox(t *testing.T) { reviewOutbox(t, true) }
```


## 10. 用户反馈后的 EnsureStorage 服务 Hook 方案

### 推荐职责分配（设计建议，尚未实施）

用户建议把存储初始化声明放到 service.go，由框架自动调用，而不是让每个 main 记住 models.EnsureStorage。此方向更适合作为框架标准：服务声明自己需要的初始化，models 保留具体表清单与存储选择，框架负责调用时机与失败处理，main 仅组装服务和启动。

不建议直接把 EnsureStorage 加成 IService 的必需方法。当前 `pkg/server/types/server.go:58–65` 的 IService 只有 ServiceName/Routers，IStartService 是另外的可选接口；直接增加必需方法会让所有既有消费方及无业务存储服务编译失败。建议采用同样的可选能力接口，命名在实现前按项目约定最终确定，例如：

```go
// IEnsureStorageService 声明服务在接受请求前必须完成的存储初始化。
// 以下是待实现接口草案，当前仓库尚不存在此接口。
type IEnsureStorageService interface {
    EnsureStorage() error
}

// EnsureStorage 只代理 models 组合根，不在 Service 中选择 DB 或逐个建表。
func (s *Service) EnsureStorage() error {
    return models.EnsureStorage()
}
```

数据库名、IDataAction、表清单、框架自动建表补列的细节均继续属于 models；不能借此把迁移、GORM AutoMigrate 或逐表 SQL 移到 Service Hook。无存储服务可以不实现该接口；标准示例中有存储的服务应明确实现，并由契约测试防止遗漏。

### 当前启动链路与 Hook 位置

现行启动源码为：`AddIService` 创建并注册 ServiceContext（server.go:213–219）→`WebServer.Start` 调用 initServer（:282–326）→initServer 校验最终配置并构造 HTTP/gRPC/HTML 运行组件→ServiceGroup.Start（:401）→各 Context 运行状态齐全后 serviceStartContexts 以 goroutine 调用 IStartService.Start（:205–209）。后者已经太晚，不能承载存储屏障。

推荐在 initServer 的准备阶段：先取得本次启动的确定服务快照，完成服务选项/参数应用、拓扑与配置校验；随后同步遍历已注册服务的可选 EnsureStorage Hook；全部成功后才构造可能绑定端口的 HTTP/gRPC/HTML 组件并启动监听。不能只在 AddIService 时调用：那时后续 ServerOption/启动参数未必应用完，而且调用 AddServiceContext 的入口会被漏掉。也不能只在 ServiceGroup.Start 之前、却已经创建 gRPC listener 之后才调用。

该位置是源码审查后的方案建议，不是当前已有行为。框架自己的 ManageControlPlane 初始化已有独立组合根，不能把新业务 Hook 描述为已经覆盖所有系统初始化步骤。

### 失败、生命周期与兼容性

- 方法必须同步返回 error；框架携带服务名称和 cause 终止整个启动，不仅记录日志或把失败当成功。现行 Start 是无返回值 API，可沿用其初始化失败机制；若将来增加可返回 error 的启动 API，应是单独加性接口，不能顺带破坏签名。
- 初始化失败释放本次已申请的连接、lease、事件运行资源等，不能留下声称 ready 的节点；不自动回滚已经完成的非破坏性 schema 建库/补列，也不删除业务数据。
- 同一服务实例的启动初始化有明确一次执行边界；models.EnsureStorage 本身仍需幂等。重复 Start、同名注册、多次组合或并发启动不得触发失控重复初始化；失败后不通过请求自动补偿，进程重启是否重试应由启动生命周期决定。
- 多服务启动的调用顺序应确定；相互依赖的业务 seed/资源装配与 schema 初始化分开，不能靠 map 遍历顺序或在 Hook 中调用尚未启动的远程服务。
- 动态添加服务若被允许，也必须先经过初始化屏障再接受请求；不能存在通过 AddServiceContext 或其他启动入口绕过 Hook 的路径。
- 04/07 的可靠写 runtime/Badger 绑定属于资源 readiness，EnsureStorage 只解决存储结构初始化，不能因此声称资源已全部就绪。不要改变旧 IStartService 的后置语义来兼任同步屏障。
- 先实现并验证框架 Hook，再迁移01–09服务与移除 main 重复初始化；未迁移或旧版本框架仍按现行 models.md/示例显式调用，不把提案提前写成已发布事实。实现时同步更新权威 skill、公开兼容契约与 README。

### 未来实现的行为验收（本轮未执行实现或这些测试）

1. 服务在 service.go 声明 Hook 后，main 不显式调用 models.EnsureStorage 仍可从空库正确启动。
2. 构造可控慢 Hook，在其完成前所有业务 HTTP/gRPC/HTML 请求不得进入 Parse/Validation/Do；配置校验失败也不得先访问业务存储。
3. 多服务任一 Hook 返回明确错误，整个启动失败；其余服务不开始监听，错误保留服务名和 cause，本次运行资源可收束。
4. AddIService、AddServiceContext、重复 Start、初始化中 Stop、并发注册等入口都覆盖到屏障，不泄漏监听器、lease或初始化状态。
5. 未实现可选接口的旧 IService 仍能编译与运行；旧 IStartService 行为不变，有存储的标准示例提供声明契约测试。
6. 跑真实空库建表/补列、数据库故障、启动回滚、race、API兼容与 release-contract；EnsureStorage成功与可靠写资源ready分别记录，不能互相替代。

本节仅修改审查建议，不增加接口、不迁移任何 service/main、不修改现行规范正文，遵守用户“不要修复，所有问题与建议写入文档”的范围。


## 11. 用户反馈后的子行 modelState 三态处理契约

前端在编辑过程中记录新增、修改、删除，父表提交时附带子行与 modelState，由后端按状态执行，符合当前设计。源码 `web/admin/src/components/WayPlus/WayForm/index.tsx:289–303` 在提交时将 isnew 行标记1、其他当前行标记2、removeRows 标记3并加入提交数组。现行代码把所有非新增当前行标为2，并非仅修改过的行；这一范围必须在协议中明确，不能默认已经实现字段差异跟踪。

| modelState | 含义 | 后端处理要求 |
| --- | --- | --- |
| 1 | 新增 | 校验新行内容，服务端确定父ID及持久ID，执行 Insert；不得因客户端 ID 非零就转为 Update |
| 2 | 修改 | 按持久 child ID 加载旧行，确认旧行属于本次父记录，校验后执行 Update；不得把不属于本父的旧行重新赋父ID后更新 |
| 3 | 删除 | 对已持久化行核实原归属后 Delete；新建未落库行通常前端直接移出数组，无需提交持久删除 |

0或未声明状态是否代表“不变”，应由公开协议明确；未知状态不能悄悄进入修改/新增路径。当前 SaveLines/SaveSpecs 使用 default 分支处理2及其他状态，是协议收紧建议，不额外计作已复现缺陷。

### P1-02 为什么在 modelState=2 下仍成立

复现请求编辑父B，提交父A的真实子行ID，modelState=2。因此后端进入修改分支是预期行为；但 SaveLines 先执行 line.ItemID=B.ID，再按该子行ID直接 Update，没有以数据库中原 ItemID 判定归属，于是把A的行搬到B。modelState只能表示“客户端请求修改”，不能证明“客户端给出的行属于B”。正常前端只选B的行不能替代服务端验证；本项没有声称正常前端每次编辑都会搬行。

建议修改/删除使用已验证的 `child.ID + parent.ID` 条件写并核对受影响记录；客户端 ItemID不能授权更换父归属，跨父迁移若有业务需求应独立受控命令。全部父子写入应由 models 事务统一提交，任一行状态、归属或字段失败则全部回滚。

前端 edittable.tsx:79–84 给新行生成时间戳 id作为编辑标识，但后端 state=1且ID非零时直接 Update，这与新增语义存在冲突，已在R06记录。建议分离UI临时key与持久ID，服务端新增始终按新增处理；该前端完整提交路径仍需真实payload与DB回读验证，不能把源码线索写成已完成浏览器复现。

本节补充状态契约与审查解释，未修改前端、后端或测试实现；没有新增运行验证结论。


## 12. 用户反馈后的业务状态与框架能力边界

P1-04 属于07订单应用示例的业务实现缺口。原审查范围包含“基于examples的应用演示检查项目实现”，因此示例里的业务问题仍应记录；不能把示例问题归为Core必须实现订单业务。

| 应用负责定义与实现 | Core提供的通用能力 |
| --- | --- |
| 订单/支付状态、合法状态迁移、支付类型与金额校验 | 通用数据访问、事务与错误传播 |
| 业务幂等键、幂等作用域、相同请求与冲突请求的判定 | 唯一约束/条件写可用的存储访问能力，以及通用可靠写基础设施；并发使用策略由应用落实 |
| 已取消订单能否支付、是否允许多笔支付、退款/分期策略 | Event center、Outbox发布、MQ可靠投递等机制 |
| EventID/Inbox去重及业务副作用的原子幂等 | 事件信封、传输重试、发布确认等基础机制；不推断业务处理结果 |

Core的可靠重试、Outbox发布幂等或Broker ACK不等于支付业务幂等，也不能自动保证订单状态合法。应用应使用这些能力实现自己定义的约束；状态机或支付模型不应因此新增到pkg/server、pkg/persistence。

P1-04当前证据仅证明07现有业务函数允许cancelled+paid和替换已有支付标识，不能证明任意应用都应禁止多次支付。后续修复目标应限于07示例的业务语义与验收；若该示例有意仅做无约束状态模拟，需要明确缩小演示承诺，不能声称已示范完整支付状态与业务幂等。

本节只澄清审查问题的归属与建议，不修改任何框架或示例业务实现。


## 13. 用户确认：删除后自动重建是08的预期演示行为

用户明确说明：“不用修复，这样做是为了演示方便，删除后自动重建”。据此撤销原P2-01的缺陷定性、修复优先级与“初始化仅执行一次、Search必须保持删空”的建议。本项保留为演示行为说明，不再计入缺陷数量；当前有效缺陷为5项P1、1项P2，编号保留原编号以便追踪，不把P2-02改号。

08的 `CatalogItemManage.SearchAfter → CatalogItem.LoadChildren` 在种子条目没有明细时调用 DefaultLinesFor/SaveLines补回演示明细，Specs有同类处理。用户删除sku-phone-case全部明细后，再次查询出现“黑色、白色”，正是该演示预期；这是08应用的演示数据逻辑，不是Core的通用查询默认行为。

此前探针断言“删除后查询应保持空”是审查者未经确认的期望，虽然真实HTTP确实观察到重新创建明细，但行为本身符合当前需求。因此该断言失败不能再作为缺陷RED；历史日志和探针保留作审查纠正记录，不能为让它转绿而改变示例行为。08其余父子部分提交、跨父修改、复制搬行三项独立问题不受此确认影响。

保留自动重建实现，不修改代码、不增加禁用开关、不替换为一次性seed初始化。后续若优化批量加载等性能，也须保留此演示语义。

## 14. 用户授权后的提交整理与验证

用户要求将项目未提交的工作整理完成。本次在 `fix/admin-rbac-demo-and-audit` 分支提交既有实现，提交为 `d830cc9`（完善RBAC演示并默认关闭MQ），并单独提交本审查文档。未修复本报告所列审查缺陷，未推送远端；此前各节的工作区、提交和验证状态仍表示其审查时点，不替换为本节状态。

实现提交同时记录 `web/admin` 子模块指针与内嵌前端产物，指针及 `build-info.json.frontend_commit` 均为 `b705b5b49a879517e56f973e38c129039a10201c`。提交后的 `scripts/check-web-dist-sync.sh` 通过，子模块Git工作区为空；保留此前披露的忽略文件，不执行清理。

提交前重新执行的Manage、Public、配置、MQ、08和09相关包测试通过；09真实本地HTTP集成测试 `examples/integration/09-admin-manage-rbac` 通过（20.415s）；`scripts/test.sh release-contract` 通过（candidate模式，未创建tag、未push、未发布）。首次沙箱运行因不允许监听本地端口失败，获得允许后重新运行上述集成测试与发布契约检查并通过。日志为 `/private/tmp/core-event-local-review-20261005/precommit-tests.log`、`precommit-integration.log` 和 `release-contract.log`。

暂存源码和文档的 `git diff --cached --check` 通过；包含dist的检查报告生成JS尾随空白，保留原构建字节，未手工修改产物。上述检查不构成浏览器UAT或实际Broker ACK/reclaim验证；本报告原有FAIL、Partial和NOT_RUN边界保留。
