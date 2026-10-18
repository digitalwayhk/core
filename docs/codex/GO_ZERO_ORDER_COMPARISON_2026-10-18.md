# 07 原生 go-zero 下单对照记录

## 范围与实现

按用户要求，在 `examples/07-shop-order-scale/go-zero-reference/` 独立实现下单容量闭环。应用不导入 Core 封装，使用 go-zero REST/zrpc、grpc-go、GORM、Badger 和 go-redis。实现范围、独立性检查、资源参数、等价边界和运行命令见该目录 README。

对照复用同一负载发送器和最终 requestID/OrderID 逐键核对，计时前的表结构、商品、JWT、证书由原夹具准备；原生测量时原 Core 应用全部停止。每轮新依赖，避免共享历史 Stream/消费组计数。守护停发时输入未完成，不能声称实际数据量相同。

## 前置验证

- Badger 原路径重开、重复请求、指纹冲突、并发合批、重复 ACK 不扣减其他 pending：通过。
- 用户 Token、管理域拒绝、错误签名及过期：通过。
- 真实 MySQL 中订单写入后事件构造失败：事务回滚，本地 pending 保留；恢复后重复同步仅有一份订单和 Outbox：通过。
- 真实 Redis 中同事件重复消费只记录一次，合法消息 ACK，错误 schema 留在 PEL：通过。
- 上述测试的 `-race` 检查：通过。
- 原生依赖只包含本目录代码和底层库：通过。
- 7,000 笔烟测全部确认并匹配，最终订单/Outbox/published/entries-read 为7,001（含一笔计时外准备订单），pending/lag/PEL为0：通过。烟测不代表容量验收。

烟测首次因配置时长字段使用标准 JSON 解码而启动失败，改用 go-zero 配置加载器后通过。真实事务测试首次因测试订单缺少 acceptedAt，被 MySQL 严格模式拒绝；补齐夹具后验证了预期的事务内故障回滚。原始失败与复验日志保存在 `.temp/go-zero-compare-20261018/`，没有覆盖失败证据。

## 结果状态

正式短窗与长窗对照尚在执行准备阶段，当前记为 **NOT_RUN**。不能据烟测推导 Core 或 go-zero 更快，也不能归因底层框架。

## 解释限制

Core 已使用 go-zero HTTP 和 zrpc 客户端；本次比较的是原生应用方案与 Core 标准方案的整体差异。原生不实现 Core 的注册自愈、身份账本、通用 pending 版本/墓碑、运行图和统计任务；zrpc 原生入站中间件与 Core 自管 grpc-go 不完全一致。原生使用更简单的本地数据布局；两者都保留 fsync、异步同步、SQL事务Outbox、消费去重及成功后 ACK。

当前负载没有支付、取消、后台管理、配置变化、列表缓存命中或活跃 WebSocket，原生没有复制这些能力。本次结果只适用于下单及订单创建事件路径；任何差异不能直接归因为 go-zero 引擎，也不能证明整个框架的替换收益。
