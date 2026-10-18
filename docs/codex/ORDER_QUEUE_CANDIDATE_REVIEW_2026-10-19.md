# 队列优化候选审查（执行中）

由当前主Agent按仓库要求直接自审，未调用子Agent；这不是独立评审或最终验收。代码基线为 `ae64843`，真实运行与完整原始结果位于 `.temp/order-queue-repair-20261019/performance/`。

## 已发现的候选接入问题

### D2首个接法不能启动

`overlay-d2-v1-prepare-init-rejected/` 将 `MySQL.getGormConfig` 的 `PrepareStmt` 直接改为true。Core在新库初始化期间执行 `ensureDatabase` 的 `USE`，MySQL返回1295：该命令不支持prepared协议。`formal-d2-native-r1` 因准备Core schema失败而没有开始发压，不能当作原生吞吐失败或填写0吞吐。

保留v1的源码、哈希、失败日志和可重放overlay。v2保持初始化时PrepareStmt=false，在newDB成功完成建库、切库、配置连接池后为业务SQL开启Session的有界缓存（最多1000项，TTL一分钟）；原生也保持同样缓存上限和TTL。v2仍是实验，不改框架默认值。原生r1必须补跑v2，v1不计入三组性能比较。

### D4不能只以静态读性能决定保留

`order-service/business/order_rule_cache.go` 的正向缓存无TTL，只有 `Invalidate()` 会清空；当前07实际代码没有调用此方法，`order_rule_cache_test.go` 的更新测试是手工失效。`api/public/create_order.go` 将这个缓存保存在路由实例依赖中，请求池复用同一缓存；`business.SaveOrderRule` 只提交权威库，没有在此调用失效。

因此，预置默认规则可能改变无规则场景每次查库的成本，但不能据此声称配置更新、禁用和恢复已端到端通过。原生README也已明确没有复制配置事件失效。D4只用于定位规则读取成本，尚不能保留为应用默认优化；不得添加负缓存、改变“禁用规则查不到后使用内建默认值”的现有业务含义，或临时删除管理能力换取性能。

后续取舍需要单列这一现有应用接入缺口。修复时应先明确并测试实际的配置传播契约，再接入相应失效和并发版本保护；不能把手工调用Invalidate的单测作为真实管理更新验收。

## 测量与资源边界

首轮启用profile的诊断与无profile正式短窗分别记录。D1首组先导更快，而新计时口径下的第一组原生D1 p99反而高于基线；尚未证明稳定收益，不据此改默认池参数。

D3独立驱动专项通过utf8mb4下的引号、反斜线、Unicode、NUL、二进制、NULL、精确decimal和int64参数往返及事务内查询/rollback。它不是事务写入回滚测试，也不能推导其他字符集可安全启用插值。候选如保留，仍须通过业务事务和完整回归。

本机为M3 Max、16逻辑CPU、64GiB内存；Docker为8 CPU、约32GiB内存。没有停其他桌面应用或其他项目，因此结果只代表本机共享环境。每轮独占MySQL/Redis；通过轮完整物理归档、逐字节比较及所有权复核后回收原测试卷，失败卷和旧验收数据保留。已选一个通过卷完成真实恢复启动，65000个确认请求逐键匹配、缺失0；其他卷仅完成归档逐字节比较，不泛化为全部已恢复。

## 发布阶段的后续单变量

Core首组30秒基线累计load=3.831秒、mark=14.900秒、publish=95.646操作秒。D3对应2.451/13.372/90.311。发布累计含4路重叠，不当成壁钟；取批与确认在发布循环中串行，确认成本值得独立测量。B1只比较Outbox批次100→512，保持SQL基线、发布并发4、消费并发8、同key屏障及同事务确认。原生与Core都按同样批次处理，Core仅在编译overlay里接通既有event.BatchSize到ServiceContext入口，尚未保留API或应用改动。两轮60秒先导整体失败，不据排空改善宣称容量通过；正式比较改为匹配原基线的30秒三组。

已在D1/Core r2和D2/Core r3两次重现user metrics连续三次750ms超时；两次SQL/Broker及其他进程观测无同类错误。D2现场成功捕获三个进程栈。user进程264个goroutine等待日志互斥锁，两个持锁者正向普通日志文件执行Write；没有抓到阻塞中的Prometheus处理栈。错误日志首次出现在守护取消后的05:16:27，先前三次超时在05:16:23～25，不能倒推取消后的日志风暴是此前根因。user日志05:16:22后到27有空窗，其他进程同期仍可观测，具体停顿原因尚未定位。保留原始失败和栈，不放宽750ms、不忽略缺失，也不凭空修复Collector。

## 共享MySQL池认证隔离：已复现并独立修复

`pkg/persistence/database/oltp/mysql.go:getConnectionKey` 仅使用Host、Port和最终Name。同进程第一次以账号A打开某地址/库后，账号B或错误密码使用相同键，`GetDB`直接返回A的缓存连接，未重新按B认证。不同数据库凭据不能作为共享池的隔离边界。

`.temp/order-queue-repair-20261019/mysql-pool-identity-audit.log` 的真实只读对照结果：同一pool指针=true；Core错误凭据连接成功=true；直接驱动拒绝相同凭据=true；借用连接仍是第一个账号=true。探针只查询CURRENT_USER，没有创建用户、表或修改业务数据。此处是复现出的框架问题，不是根据Key字符串作出的猜测。

当前性能用例均使用同一隔离root账号，不将这个缺口解释为前述metrics超时。全部对照封存后，`816fccd` 独立修复连接池及同步限流身份键：同配置复用，不同认证/驱动语义分池，以随机进程密钥HMAC摘要避免泄露密码或固定可猜摘要。`pool-identity-red-r3.log` 在旧实现真实失败，`pool-identity-green-root.log` 和 `pool-identity-consumers.log` 在实际源码验证MySQL、事务/恢复、控制面身份及race通过。第一次r2因沙箱无法访问构建缓存而未运行，不计为RED。后续性能必须重新封存源码，不将安全修复混为D1/D3测量收益。

## 额外边界与建议

- 07的 `OutboxStore.LoadPending` 丢弃了ctx，`PendingOutbox` 的只读入口没有接收调用方上下文。慢SQL或网络阻塞时，发布器取消无法直接取消这次读取。当前未做该故障的定向复现；应另以现有上下文数据访问入口和慢查询取消用例验证，不能因为正常负载通过就声称退出取消边界已覆盖。
- 本轮Redis保持AOF everysec，Core发布路径只等待XADD命令返回。[Redis官方持久化说明](https://redis.io/docs/latest/operate/oss_and_stack/management/persistence/)明确这种配置存在约1秒灾难丢失窗口。真实ACK/reclaim和应用进程硬杀通过，不等于Broker主机断电零丢失；本轮不改变基线耐久配置，也不把这一配置的容量结果外推为更强Broker持久确认保证。
- 大负载验收按已确认请求逐键匹配SQL事实，消费收敛按lag/PEL观察；没有逐EventID的持久用户Inbox核对。07用户层仍是进程内去重facade，不能用计数收敛宣称完整生产消费方迁移或永久消费状态。

## 发布门禁的共享Broker观测隔离

第一次D3发布候选门禁的config-contract按默认包并行使用同一专用Redis，`TestLifecycleWorkerThirteenByFortyFour/redis` 的全局INFO commandstats读到evalsha增量1699，阈值396，导致失败；XINFO/XPENDING各132保持。该计数按Redis实例累计，不能区分其他并行包的命令。单独运行同一测试两次，evalsha均220、完整扫描均132、错误0，原断言保持，NATS明确SKIP。原失败保留为 `sql-options-release-contract.log`，独立复验为 `mq-lease-recheck.log`。

`GOFLAGS=-p=1` 串行包执行完整门禁已PASS（88.644秒，`sql-options-release-contract-r2-result.json`）；原失败保留，生产逻辑及计数断言均未修改。共享Broker的全局命令统计必须串行或每包独占Broker，不能靠放宽倍数、跳过真实Redis或删除失败记录解决。
