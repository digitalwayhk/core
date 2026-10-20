# Redis Streams 连接池饥饿专项实施计划

## 范围与基线

当前基线 v1.4.4 / 668e491；现场消费方 v1.4.2、go-redis v9.21.0。只改 Provider 单实例连接预算/生命周期、配置传递及观测，不新增/删除/调整跨实例 owner 机制。既有 SharedKeys 不作为本轮解决方案，不修改 Bitzoom、不连接 Full-E4、不清现场数据。用户文本同时含“验证后发1.4.5”和后续“不要自动发布”，先完成候选修复与验收，发布动作需消除这一冲突后执行。

## 已核实根因

串行可靠读取 BLOCK 200ms、普通订阅 BLOCK 2s 与所有非阻塞命令共用 client。go-redis 9.21.0 先在 Get/waitTurn/FastSemaphore 排池名额，再发 Redis 命令；这不是 Lua 服务端执行慢。分键调度已用无 BLOCK 的 Lua，仍可被其他主题的阻塞读取挤占共享 client。现场两轮池等待分别90.2%和94.9%，不能将累计窗口数据当单条耗时；现场未读取初始化 Options，池上限未知。

## 最小实现

- 每个 Provider 两个成熟 go-redis Client：命令池负责 owner/ACK/发布/生命周期及非阻塞分键 Lua，阻塞读池只负责现有阻塞 XREADGROUP。不开每订阅独立池，不改变消息、组、游标、保留或业务payload。
- 最小加性 RedisStream 配置 CommandPoolSize / BlockingReadPoolSize，零值分别8/16，固定预算不依赖 CPU。同时设置 PoolSize=MaxActiveConns；按需建连。旧构造器保留，新增有选项构造器，负数和超出客户端范围明确拒绝。
- 连接数按每实例命令C+阻塞B计，不随订阅数无限增池；订阅超过B时只在阻塞读池背压。KeyConcurrency 不决定全局连接上限，控制流自身仍可能有负载背压。
- Close 先取消，再关闭阻塞池打断I/O，等待现有worker，最后关闭命令池保留原owner释放语义。取消清理订阅登记，注册与Close的WaitGroup互斥。保留实际handler退出等待，不宣称杀死任意业务调用。
- PoolStats 按command/blocking角色提供总/闲/用、队列、等待次数/秒、超时和预算固定指标。未连接/未采集不填零；9.21等待时长只累计成功取得连接的排队，不冒充所有取消等待。

## 验证与交付

1. 已有真实RED：CPU1/2、相同总预算下多空闲订阅饱和，续租/校验/发布/fenced ACK失败；配置、负预算和指标失败。原件单独保留，不以夹具编译错误当RED。
2. 保存旧源码测试二进制，在相同CPU/订阅数/总预算/handler下测serial和keyed吞吐、池等待及发布/读取/handler/ACK时钟；新旧相同测量窗口，不设业务100ms阈值。
3. GREEN 后覆盖取消/Close、慢handler/错误、断连恢复、pending接管、大量订阅启停、真实ServiceContext→EventBridge及既有顺序/跨窗/生命周期回归，跑对应包race与配置/公开API/发布契约。
4. 更新能力矩阵、唯一权威skill、中文根因/预算/升级与实际命令结果；生产迁移/吞吐、Redis生产拓扑及未执行项明确NOT_RUN。保留主目录原有未跟踪内容。

## 实施与审查记录

上述修复与专项验收已实施；主Agent按正确性、资源、接口兼容、依赖与日志边界自审（仓库禁止子Agent，不称独立审计）。审查发现父context取消后登记残留，补充三种订阅真实RED，再以工作者退出调用幂等stop修复，完整五包普通/race/vet再次通过。示例06三进程三角色订单可见性race通过。修改、对照与NOT_RUN详见[专项记录](../../codex/REDIS_STREAM_POOL_ISOLATION_2026-10-20.md)。本计划不代表合并或发布授权。
