# 归属Runtime内独立顺序组与共享fence实施计划

> 本会话主Agent使用executing-plans逐项执行并自审，不启动新Agent/Task/终端。用户已授权先计划/自审再实施和正式发布，不重复询问是否开始；原业务现场和WIP保持。

**目标：** 同一租约内按应用不透明group有限并行、真实独立完成与恢复；正式新增共享Check/排他Activate合同，原Step与排他路径零值兼容。

**架构：** 既有ownership.Runtime仍管理凭据、恢复、观测、取消和工作发现；显式组模式使用同实例唯一共享执行预算，没有应用Submit接口或另一套持久队列。SQL仍是原事实，group回调进入固定worker后才查询/claim原组首。新共享fence是独立显式API，不隐式降级原锁。

**技术：** Go现有context/chan/atomic、既有Store/Redis、原ServiceContext资源管理、MySQL/GORM锁定读、Prometheus。无新依赖。

## 已确认基线与备选

当前main 4de280f26dbf417ee283455b523fd298a3d7824a(v1.14.1)，干净；消费方v1.14.0差异仅观测。本轮只读已授权handoff，不访问其他消费方文件或现场。默认工作区、未发布锁/Pebble/PostgreSQL/Kafka分支均不合入。

- 改归属key为每业务用户：会改变目录/fence及迁移语义，不作为本轮捷径。
- ConcurrencyTasks等批池：整批Wait、没有独立完成/租约子任务退出合同，不采用。
- 推荐：原Runtime显式组模式及一个实例级共享预算；旧Step保留，两类SQL锁合同分别实现与验收。

## 公开合同（本轮拟新增，不是现有API）

```go
type GroupWorkBudgetOptions struct { Workers int; Prefetch int }
func NewGroupWorkBudget(instanceID string, options GroupWorkBudgetOptions) (*GroupWorkBudget, error)
func (b *GroupWorkBudget) Close(context.Context) error
func (b *GroupWorkBudget) Done() <-chan struct{}

type GroupWorkRequest struct { Limit int; ExcludedGroups []string }
type GroupWorkOptions struct {
    Budget *GroupWorkBudget
    NextGroups func(context.Context, Credential, GroupWorkRequest) ([]string, error)
    StepGroup func(context.Context, Credential, string) (bool, error)
}
// 在原Options追加GroupWork *GroupWorkOptions；与原Step二选一。

// ServiceContext每实例只允许创建一个，自动登记资源与指标；所有scope共用返回句柄。
func (sc *ServiceContext) NewOwnedWorkBudget(ownership.GroupWorkBudgetOptions) (*ownership.GroupWorkBudget, error)

type SharedFenceTransaction interface {
    LockFenceShared(scope, key string) (FenceState, error)
}
func CheckFenceShared(SharedFenceTransaction, Credential) error
```

预算：Workers必须1..64，Prefetch必须Workers..1024；Prefetch是含选择预留/排队/执行在内的总工作身份额度，不是额外每scope一份。group非空且最多512字节；同预算最多4096个租约登记，与既有每实例最多64scope×64持有槽一致。直接StartRuntime使用者也须同实例共用唯一预算，句柄拒绝不同InstanceID。正式ServiceContext入口拒绝重复预算资源；预算无公开Submit/队列接口。

NextGroups只在原SQL中发现可执行组身份，不得claim或执行、不得加载无限payload；最多Limit项，必须排除ExcludedGroups。StepGroup仅在Core已独占(scope,key,group)且实际固定worker接纳后查询/claim原最早未解决项，并复用应用原持久化/幂等/失败分类。group顺序作用域是同scope/key，不保证跨scope/key全局用户顺序。返回true,nil仅表示真实提交；false或error不产生确认，不自动更改attempt、跳过或补偿。

## 执行、失败与退出设计

1. 原runKey完成Activate→Restore→Ready后进入新组协调分支；原Step分支不改。每个key仍是原协调循环，不建立应用额外worker或持久队列。
2. 共享预算一次创建固定Workers个执行goroutine；队列仅有有界group身份/原凭据引用。选择前预留有限Prefetch额度，单轮不超过Workers；返还未使用额度。重复/超量/过长/被占组拒绝，不假成功。容量等待使用可取消通知/额度，不忙循环。
3. group从选择排队到回调实际退出始终占用；同group后继不会提前进入StepGroup查询/claim。B1实际退出并成功后立刻释放本组、更新真实进度并唤醒发现，B2无需等A1；没有整批wg.Wait。失败/无进展采用原Poll技术间隔，应用SQL仍决定重试资格/PAUSED/未知，不新增业务重试策略；期间有限槽可能背压。
4. 每个StepGroup有独立Execution context。整个持续排空循环不套30秒计时。单元超时取消该单元，真实未退出仍占group和worker，不补替代worker；其他健康组可用剩余槽继续。工作超时计数独立，原控制阶段、续租、观测/聚合Stall保护保留。若全部槽挂住，真实背压/停滞仍撤销，不承诺强杀业务代码。
5. 原控制stage的Execution仍适用于Activate、Restore、NextGroups；不把持续排空循环的年龄当作执行年龄。新实际组在途必须反映到观测，不能借BlockedDue覆盖。LastConfirmed只由未取消且真实成功的工作更新，enqueue/wake/查询不算。
6. group任务绑租约context，执行前再验证原Ready凭据；失去租约停止准入、取消排队/在途，Close/Done等待NextGroups和实际StepGroup退出。预算Close同时取消登记租约、排空未执行身份并等所有固定worker及租约退出，ctx只限制调用方等待。没有每单goroutine或后台逃逸。
7. 引用revision真的变化且需要Restore时，本key暂停新准入并等自身实际工作退出再Restore；这是显式恢复维护屏障，不是每批屏障；其他key可继续。初始恢复仍在Ready前，未退出工作受独立执行/停滞约束。无Restore回调时不引入该维护等待。
8. 资源在启动组合根先登记预算、再登记Runtime，逆序先Close Runtime再Close预算；初始化失败关闭已创建资源。预算只输出固定低基数组件指标，单实例不按每Runtime重复相加全局预算。

## 共享fence独立合同

保留FenceTransaction.LockFence的排他要求与旧CheckFence/ActivateFence；CheckFenceShared仅调用新LockFenceShared。它必须在真实业务SQL事务内对已初始化的唯一(scope,key)行取得共享锁定当前读，校验完整凭据，保持至commit/rollback；不是普通快照SELECT或缓存。Activate仍排他等待所有旧共享事务结束，恢复/Ready之后旧epoch/token拒绝。账户/任务行排他锁、用户前序、未知commit及幂等由应用保留，不由技术读锁替代。共享接口不含SaveFence，不能把Activate统一换共享锁。

## 文件责任

- 新建ownership/group_work_options.go、group_work_budget_options.go：公开类型、有限预算及组合验证。
- 新建ownership/group_work_budget.go、group_work_job.go、group_work_lease.go：原Runtime共用执行面，固定worker/额度、组占用、实际退出。
- 新建ownership/runtime_group_work.go：原runKey的新显式协调分支；没有公共通用Submit。
- 修改ownership/options.go、runtime.go、runtime_key_progress.go、key_status.go：分支接线、真实在途/进度、控制与单元执行区分，旧默认保留。
- 新建ownership/group_work_metrics.go、修改observability/provider.go：全局预算组件一次注册，固定名称，与真实Collector一起测试。
- 新建ownership/fence_shared.go：正式共享校验；旧fence.go只按需复用完全等价私有比较，不能改旧锁选择。
- 新建router/owned_work_budget.go，最小增加ServiceContext私有预算句柄；管理唯一预算、指标及逆序资源关闭。
- 扩展现有ownership真实MySQL/进程fixture与tests，新增组执行/关闭/容量/共享锁测试；不新建测试框架。
- 更新权威skill scoped-ownership、multiservice-and-observability、testing-and-release，正式SQL任务/持续归属指南、API/兼容矩阵、例11 README与CHANGELOG。

## 任务与实际验收顺序

- [ ] T1 固定真实RED：在现有真实Redis Runtime中A组阻塞、B1完成后B2尚不进入，保留旧Step证据；共享锁用两个旧Check事务同guard互相阻挡保留RED。缺新符号的编译失败不算产品RED。
- [ ] T2 实现预算/组模式最小GREEN：A保持占用、B连续推进；同group后继不先StepGroup；不同Runtime共享预算，总执行≤Workers，全部身份≤Prefetch，非法选择拒绝。
- [ ] T3 生命周期和时钟：默认Execution30秒持续健康流量超过30秒不误杀；单元超时未退出不释放槽；取消/失去Ready/Close/预算关闭、迟到完成不假确认，全部挂住明确背压；失败/暂停组不跨过前序，健康组继续。
- [ ] T4 真实MySQL S/X：多组共享事务共存、Activate等所有旧S事务、激活后旧凭据拒绝；旧X路径仍串行；先前快照不能替代当前读。未知commit由原SQL事实幂等，不自动判失败。
- [ ] T5 独立PID原schema恢复：真实SIGKILL、原pending/attempt/fence保留，新owner等旧真实SQL事务退出；无新请求也排空，旧会话不能确认新状态，Close超时不算成功。
- [ ] T6 实际小型同机SQL对照：相同SQL事实/连接预算/可靠性，对照旧单key Step与新显式模式，记录吞吐/分位/错误/排空及真实上限；不把该组件对照当Full500/s或容量验收。
- [ ] T7 对应包普通/race、真实Redis动态/固定迁移与旧Step/Ready/观察保护回归；例11真实多进程/mTLS/WS/SQL按原门禁运行。保留所有FAIL/SKIP/NOT_RUN。
- [ ] T8 文档/公开兼容/配置/安全/内嵌前端/正式发布门禁；独立提交，核远端合法MINOR版本、不覆盖tag/不force；main及annotated tag原子发布，官方Proxy/SumDB全新缓存验包。失败原件保留，不以本地缓存冒充官方包。

每项用rtk代理，原始argv/退出码/bytes/SHA封存在/private/tmp/core-owned-sql-independent-groups-20261118/；不读其他消费方资料、不部署/压测Full-E4。正式新公开能力应MINOR，实际版本以发布前远端核验为准。

## 自审（实施前）

- [x] 业务粒度：Core仅接不透明group；所有业务状态与原SQL仍由应用负责，无新MQ/表/本地pending。
- [x] 资源乘法：预算句柄实例唯一，多scope共用；不能每桶N个执行worker，也无公开Submit给应用自造另一循环。
- [x] 30秒误杀：单元context与整个排空寿命分离；旧控制Execution及聚合Stall保留。
- [x] 假进展：选择/领单/入队不写LastConfirmed；未知/取消/迟到不确认；真实失败错误仍保留应用处置。
- [x] 真实退出：group/worker直到实际回调返回才归还；Close ctx到期只表示等待超时；不替补不合作旧工作。
- [x] 同组前序：选择只发现身份，StepGroup接纳后才查询/claim；占用组排除并再次验证，无整批完成屏障。
- [x] 锁合同：显式共享Check与原排他Activate分离；持锁当前读、账户X锁、唯一初始化行和未知提交分别回归，不笼统把LockFence换SHARE。
- [x] 兼容：Step/共享组MQ/持久格式/指纹/原SQL事实不改；新模式显式二选一，非法配置失败关闭。
- [x] 维护恢复：revision变更才有本key恢复屏障，稳态没有批屏障；不让旧工作与新Restore不安全重叠。
- [x] 证据层级：原R555统计非Core重跑，不承诺13秒全部由本修复解释；Full业务/500/s/容量仍由消费方验收。

自审结论：按以上最小显式模式实施。单元超时的隔离是新模式的明确合同，不弱化原Step默认；共享锁是单独新承诺，未验收前不发布。实现若发现契约冲突，先回传精确事实，不删边界凑GREEN。
