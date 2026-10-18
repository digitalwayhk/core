# 队列优化候选审查（执行中）

由当前主Agent按仓库要求直接自审，未调用子Agent；这不是独立评审或最终容量验收。单变量测量基线为 `ae64843`，最终代码为 `258b45a`，真实运行与完整原始结果位于 `.temp/order-queue-repair-20261019/performance/`。最终全量和发布门禁通过，容量阶梯存在FAIL，600秒长窗另列。

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

D3最初的独立驱动专项仅覆盖参数往返和事务内查询/rollback，不能代替事务写入回滚。正式保留前已补齐真实utf8mb4危险字节、二进制、NULL、精确decimal/int64及写事务commit/rollback，完整OLTP/race、07和最终全框架通过；仍不推导其他字符集可安全启用插值。

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
- 最终Core长窗user有724343条`Auth required for api`的固定INFO日志，另有安全访问日志；日志体积约385MiB。前者只是路由认证配置描述，重复写入值得单独用保留真实安全访问日志的控制实验衡量；目前未量化删除它的端到端收益，不将“发现重复日志”当成本轮停顿根因，也未关闭认证或访问审计。
- 大负载验收按已确认请求逐键匹配SQL事实，消费收敛按lag/PEL观察；没有逐EventID的持久用户Inbox核对。07用户层仍是进程内去重facade，不能用计数收敛宣称完整生产消费方迁移或永久消费状态。
- 两侧user facade都长期保存请求幂等映射及事件去重集合：Core的`userOrderIDByRequest`、`processedOrderEvents`与原生的`requestIDs`、`processed`均为没有回收路径的`sync.Map`。不同请求/事件持续进入会持续增加内存状态；这与MySQL永久业务事实增长不同，排空pending也不会移除这些集合。容量资源审核必须单列，不能据10分钟观测或GC后堆下降承诺长期有界。后续应与业务方明确幂等/去重保留期和持久权威，再设计有界热缓存；直接加TTL或删除映射会改变现有重试/重复投递语义，本轮未擅自处理。
- `connection.go:logCleanupStats`将Go `MemStats.Sys`注释成“最接近RSS”，按[Go官方定义](https://pkg.go.dev/runtime#MemStats)，该指标统计Go运行时从系统申请的内存，不能替代整个进程RSS或涵盖Badger mmap驻留。容量资源审核使用实际`process_resident_memory_bytes`并结合堆、mmap/磁盘和容器指标，不据此日志数字宣称进程内存稳定；此处未修改统计实现。

## 发布门禁的共享Broker观测隔离

第一次D3发布候选门禁的config-contract按默认包并行使用同一专用Redis，`TestLifecycleWorkerThirteenByFortyFour/redis` 的全局INFO commandstats读到evalsha增量1699，阈值396，导致失败；XINFO/XPENDING各132保持。该计数按Redis实例累计，不能区分其他并行包的命令。单独运行同一测试两次，evalsha均220、完整扫描均132、错误0，原断言保持，NATS明确SKIP。原失败保留为 `sql-options-release-contract.log`，独立复验为 `mq-lease-recheck.log`。

`GOFLAGS=-p=1` 串行包执行完整门禁已PASS（88.644秒，`sql-options-release-contract-r2-result.json`）；原失败保留，生产逻辑及计数断言均未修改。共享Broker的全局命令统计必须串行或每包独占Broker，不能靠放宽倍数、跳过真实Redis或删除失败记录解决。

## 默认配置并发路径：自审复现并修复

在最终全量开始后，主Agent继续检查新池身份读取与既有DSN默认化的交互。覆盖层测试32个goroutine同时构造DSN和读取池身份，旧内部构造会回写调用方 `Charset/ParseTime/Loc`，race工具报告DATA_RACE，配置快照也发生变化（`pool-default-config-race-red.log`，源码 `b669adb`）。不能用先前完整非race通过代替这一新边界。

仅对本轮go test调度器发SIGINT，等待当前包完成后停止后续包；原全量记录以退出1、源码变化0、218.069秒保留，明确是主动审计中断，不能标最终PASS。`dc0c1b1`将内部DSN构造改为局部默认值并复用同一构造方法，建库仍显式采用默认utf8mb4；不改变公开 `Config.MysqlDSN` 的历史行为。真实默认建库、配置不回写、认证隔离、SQL参数/事务及全OLTP race均通过，证据为 `pool-default-config-race-root-green.log`。已从头启动最终r2全量，未继续使用旧全量结论。

凭据边界审查另复现JSON对非法UTF8替换导致不同原始字节的用户名/密码生成相同键（仅身份编码定向，不冒充真实账号认证复现）。`pool-credential-bytes-red.log`在旧编码失败；标准库长度前缀按实际驱动字段无损编码的 `pool-lossless-final-green.log` 验证用户名/密码/字符集/时区原始字节及字段边界不混淆，完整OLTP、真实SQL、控制面和race通过。通用gob编码仅作覆盖层候选，没有保留；最终长度前缀不增加依赖，也不依赖字符串分隔符。新增驱动选项须纳入池身份及隔离测试。第二轮全量同样主动中止，445.884秒、源码变化0，保留为非最终结果；最终版本须重新从头全量。

DSN覆盖入口仍沿用既有 `Config.SetMysqlDSN` 字段映射；本轮只增加显式interpolateParams及其错误处理，并未将其扩展为任意驱动DSN直通。未知参数未接线、端口等旧解析边界也未整体重写，不能用新增选项的专项通过声称整个DSN解析器已全面验证。文档已缩窄到实际行为。

## 最终阶梯中的GC停顿线索（尚未归因）

最终配置500/s两侧PASS；1000/s Core确认56746、传输失败1183、丢弃2071、metrics_0超时2次，原生60000全部成功；1500/s Core确认82313、丢弃7687、metrics_0超时1次。以上Core确认均逐键匹配、失败后落库0、物理核对一致，不是已发现的数据丢失。

1000/s user的 `go_gc_duration_seconds_sum` 在06:23:15附近从约0.167增至1.479秒，06:23:21附近从1.545增至4.017秒，后者紧邻两次metrics超时。堆仅约20～45MiB；文件描述符软限制1048575，未发现accept/EMFILE或panic日志。该证据支持调查进程级GC停顿，不能只按取消后的日志锁等待归因，也不能据此认定MySQL连接池或Badger。

已查询[Go上游#81759](https://github.com/golang/go/issues/81759)：报告Go1.26/1.27的syscall/STW竞态，含darwin/arm64复现讨论，问题仍打开。它目前只是候选机制，尚未证实本项目命中。将先在完整窗口边界暂停，使用自有进程的操作系统采样及独立标准库复现；不添加框架忙循环，不用调试开关或放宽750ms伪装修复。

后续已经完成OS原生arm64采样自检和两轮真实业务诊断。业务轮未捕捉到秒级停顿；标准库独立pipe/GC复现三次均卡住，增加100ms定时器的诊断控制三次均完成15秒。栈、SHA和六轮结果见[执行记录](ORDER_QUEUE_EXECUTION_2026-10-19.md)。这是工具链独立缺陷的本机证据，业务同因仍未证实。上游修复候选仍在审查，未擅自修补GOROOT或引入生产忙定时器；容量测试继续使用原Go 1.26.6并保留所有失败。

## 当前处理结论

- 已保留：通用MySQL池认证/驱动语义隔离、默认配置内部构造不回写、无损身份编码；07/原生idle20和显式utf8mb4插值。对应代码与定向/race/完整回归均通过。
- 不保留：D2、D4、B1，以及仅靠微基准去除全局统计GC的候选。缺少稳定端到端收益或缺少完整应用语义验证，不能扩成全局默认。
- 尚未关闭：业务进程秒级停顿归因、600秒三轮容量、07规则缓存真实失效接线、LoadPending取消边界、两侧facade幂等/去重集合长期无界。首个最终长窗在约481秒守护停发、31.249秒排空，明确FAIL；余下5轮因空间不足尚未执行。应用语义和资源缺口已明确记录，不据正常短窗宣称已修复。
- 发布边界：Badger升级的公开类型/迁移、Broker主机耐久性及07消费facade边界继续按现行契约登记；未测消费方、NATS/etcd等专项保持NOT_RUN。本轮不推送、合并或发布。
