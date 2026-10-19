# 07规则刷新与Outbox取消修复验证

## 范围与结果

用户授权先处理已确定的问题。本轮只修复规则旧快照与Outbox查询取消，不继续性能测试，不改变订单/消息事务、禁用规则含义或去重保留策略。基线为`ace1b24`，由主Agent直接实施、自审，无子Agent或独立评审。

规则已改为每次从共享MySQL权威库读取。内部订单服务不再保留正向规则快照，管理修改提交后的新读取无需事件通知或手工失效；已开始的读取仍按数据库一致性语义执行。`OrderRuleCache`、构造器及`Invalidate`保留旧调用兼容，后者不再需要做事。原生go-zero对照删除同样的正向快照；未配置/未找到启用规则时仍按既有语义回退默认值，不重新定义禁用规则。

代价是已配置规则也增加逐次只读查询。此前有正向缓存的性能数据仅属于原源码；按用户要求不再测量吞吐或声称容量达标。

Outbox取批保留`IDataAction.Load`、独立action、未发布过滤、ID升序、原分页默认/上限和既有只读连接恢复。新增可选`SearchItem.Context`，在MySQL/SQLite的普通COUNT/读取、免COUNT和统计SQL中传给GORM；nil保持原行为，JSON忽略，不改变`IDataAction`接口。07适配器在查库前拒绝nil/已取消上下文，并传递EventBridge的实际ctx。旧`PendingOutbox(limit)`兼容调用仍存在。

该上下文契约限于MySQL/SQLite查询执行，不承诺初始建连或其他适配器中断；没有把只读取消扩大成业务事务自动重放或可靠写中断契约。

## RED与已完成验证

证据根：`.temp/order-correctness-fixes-20261019/`。

| 场景 | 失败证据 | 修复后验证 |
| --- | --- | --- |
| 两个已加载读取器感知规则收紧/恢复 | `rule-red.log`：收紧后仍返回成功；权威读取失败被旧缓存隐藏 | `core-green-r1.log`，真实MySQL两个读取器和最小行为测试/race通过 |
| 真实Manage修改影响既有路由 | `rule-http-red-r2.log`：Manage Edit成功后下一单仍成功 | `rule-http-green-r1.log`：管理Add、先成功下单、Edit收紧后拒绝、Edit恢复后再次下单通过；没有手工Invalidate或重启 |
| 原生对照规则刷新 | `native-rule-red-r3.log`：更新后仍放行 | `native-rule-green-r2.log`：两个已加载实例随规则收紧/恢复变化，权威库关闭后返回错误，race通过 |
| Outbox预取消及真实表锁等待 | `outbox-red.log`：取消均被忽略，等解锁后返回nil；`outbox-context-core-red.log`：只加字段而未接SQL仍失败 | `outbox-atomic-final.log`：锁等待约150ms返回DeadlineExceeded；预取消返回Canceled，均非SKIP |
| 普通/免COUNT/统计查询取消 | `query-context-red.log`：三个分支均返回nil | `outbox-atomic-final.log`：`TestSQLQueryContext`三分支、nil兼容及JSON排除通过 |
| 保留只读连接恢复 | `outbox-recovery-red.log`：首个直接GORM候选在池关闭后失败 | 最终撤回直接GORM候选，继续走Load；`TestOutboxLoadPendingRecoversClosedPool`真实关闭池后恢复通过 |
| Outbox读取及批量确认契约 | 原有断言保留 | `outbox-atomic-final.log`：未发布过滤、升序、limit=1/0/负值、旧入口、真实批量确认提交/回滚/幂等、读取取消和恢复均通过 |

首次双副本HTTP夹具在“20条先导订单必须证明两副本都加载旧规则”的前置断言失败，聚合事实仅观察到一个实例，未进入规则更新验证；保留为`rule-http-red.log`，不是有效的缓存缺陷RED或双副本PASS。本轮改用确定性的既有单副本HTTP路由闭环，加上两个独立规则读取器的真实SQL验证，不宣称已经完成跨进程双副本管理更新验收。

原生测试前两次分别为嵌入字段初始化编译错误、夹具写入不存在的运行时列；均保留为`native-rule-red*.log`，不计行为RED。第三次才是更新后旧规则仍放行的有效复现。

## 回归与交付边界

- `regression-r1.json`：07全部包与event/router/observability串行race通过，99.079秒；这是最终上下文接线前的扩大回归，不能替代下项。
- `outbox-atomic-final.json`：最终实现完整OLTP、事务消息和07 transaction包串行race通过，14.689秒；显式开启自有MySQL，包含真实连接恢复、事务、消息确认和查询取消。
- `rule-http-final.json`：最终实现真实Manage/买家HTTP跨服务闭环通过，50.721秒。
- `final-gates.json`：最终公共API兼容通过（14.109秒）、路由/OpenAPI兼容通过（9.144秒）、日志规范通过（0.144秒）、发布候选契约通过（98.515秒）。外部未启用能力仍按自身SKIP/NOT_RUN处理，不冒充完整全框架验收。

本轮只修改示例规则读取、查询上下文接线及对应测试/契约文档；既有前端WIP和Bitzoom不操作。没有执行长窗、负载发送器或容量调参。

剩余：原始负数pending产生原因仍未找到；用户facade幂等/去重集合的持久权威与保留策略未定义，本轮不自行加TTL。生产消费方迁移、未配置Provider和完整全框架发布验收不因上述通过而被扩大为PASS。

## 临时依赖

只为功能验证创建带`core.task=order-correctness-fixes-20261019`标签的MySQL和Redis。数据目录使用有上限tmpfs，未创建数据卷：MySQL上限1GiB、Redis上限32MiB。MySQL使用64MiB buffer pool；不以此临时配置作容量或断电耐久验收。全部验证后按精确ID、名称和标签删除，清理证明保存为`cleanup.json`；用户的其他资源不在操作范围内。
