# MySQL 每操作 Ping 导致连接池放大：Bitzoom 下单容量失败案例

## 现象

Bitzoom `simple-e4` 在同一自动采集环境以 100 个已注册交易用户、20 orders/s 执行 90 秒 P2 下单时，共发起 1800 个请求，仅 197 个接纳，1603 个进入系统错误路径。端到端 p95 为 1.807745s、p99 为 2.283799s。

同一 95 秒窗口内：

- Users `saga_read` p95 173.86ms，三次 Saga checkpoint p95 约 257–286ms；
- Funds `freeze` p95 467.55ms，其中事务体 p95 375.78ms、commit p95 139.92ms；
- Positions `PrepareRiskPlan` p95 236.64ms，其中 `intent_write` p95 172.18ms；
- Users、Funds 和 Positions 同时变慢，不是单个业务 SQL 的局部回归。

## 根因

Core MySQL adapter 在已持有可复用 `*gorm.DB`/`database/sql` 连接池句柄时，仍在每次操作前执行 `Ping`。业务链路每个小步骤因此需要两次串行借连接：先 Ping，再执行真实 SQL。当多个服务共享同一 MySQL 实例并且连接池排队时，这种前置检查放大了借连接次数、排队时间和上层 deadline 超时。

Ping 也不能证明某次真实 SQL 一定成功，更不能安全决定是否重放提交结果不确定的写入。

## Core 修复模式

1. 已缓存的连接池句柄直接复用，不在每次 CRUD 前 Ping。
2. 用真实 SQL 错误识别 `driver.ErrBadConn`、`sql.ErrConnDone`、`gorm.ErrInvalidDB`、`database is closed`、broken pipe 和 connection reset。
3. 非事务只读在连接错误后驱逐失效句柄，重建并最多重试一次。
4. Insert/Update/Delete/Exec 不自动重放；连接错误后只失效句柄并返回原错误，由上层业务幂等边界决定是否重试。
5. 活动事务不跨连接恢复或重放，必须回滚后由上层重新开始。

## 禁止的推论

- 不能因为去掉 Ping 就忽略连接错误分类和断池恢复测试。
- 不能对任意写入自动重放；返回连接错误时提交结果可能不确定。
- 不能在事务内换用新连接后继续执行原事务。
- 不能只看单个包单测；必须在多服务共享 MySQL 的真实性能旅程中比较修复前后分段延迟。

## 认证证据

- 默认单测：缓存句柄不 Ping；连接错误分类；只读最多恢复一次；事务不恢复；写入不重放。
- race：全局连接管理器与 adapter 引用失效无竞态。
- 真 MySQL：主动关闭连接池后，只读自动恢复；首次写入返回连接错误且未重放；调用方第二次幂等请求使用新连接成功。
- 消费方：使用同一环境、用户池、速率、时长和 Prometheus 窗口复跑，报告 accepted/system errors 以及 Users/Funds/Positions 分段 p95/p99。
