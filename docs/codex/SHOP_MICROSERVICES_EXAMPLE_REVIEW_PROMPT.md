# 示例 06 最终外部审查提示词

请只读审查 Redis 多服务商城示例及其框架前置能力，不要修改代码。

## 审查范围

```bash
git diff 0d4a6b4..HEAD
```

设计规格：

```text
docs/superpowers/specs/2026-07-16-shop-microservices-example-design.md
```

重点实现：

```text
pkg/server/cluster
pkg/server/router/serviceresolver.go
pkg/server/mq
pkg/server/event
pkg/server/router/routerinfooption.go
examples/06-shop-microservices
examples/integration/06-shop-microservices
examples/integration/06-shop-microservices-three-process
```

## 必查项

1. Redis ClusterProvider 的注册、TTL 心跳、注销、MachineID 冲突、Watch/reconcile 和命名空间隔离是否正确；Redis 不可用时是否 fail closed。
2. `req.CallService` 是否先解析同进程已注册路由，再从 ClusterProvider 选择健康远程节点；新链路是否完全不依赖 `AttachServices`。
3. `router.WithServiceName` 是否只在 Freeze 前声明稳定目标服务名，且没有破坏既有公共 API、路由路径或目录推导兼容性。
4. Redis Streams 控制订阅是否仅在 Handler 成功后 ACK，失败是否保留 pending 并可被同组消费者 reclaim；观察事件无订阅者时是否直接丢弃。
5. 订单、支付、商品、供应商变更是否在同一 SQLite 事务中写业务事实与 Outbox；消费者是否以 EventID/Inbox 幂等收敛重复投递。
6. User、Supplier、Order 是否各自拥有模型和 SQLite；共享目录是否只有无依赖 contract 与 DTO，是否存在跨服务 import Model 或复制 DTO。
7. User facade 是否是买家唯一入口；Supplier 是否只能操作本人商品和查看本人订单；Order 是否保存商品、价格和地址快照并执行幂等下单。
8. TestToken 身份、平台管理员、买家和供应商隔离是否 fail closed；Private 缓存键是否只使用 Token 中可信 UID，WebSocket 是否按买家/供应商身份过滤。
9. 商品、供应商、订单和支付事件是否形成缓存主动失效闭环；没有失效闭环的接口是否避免启用缓存。
10. SQLite 是否在组合根、WebServer 启动前完成初始化；稳定克隆模板是否避免 Manage 全局适配器与 worker/request 之间的数据竞争。
11. 支付流水业务哈希、PaymentID/PaymentTypeID DTO 追踪和 PaymentChanged 事件类型是否正确且无空哈希冲突。
12. 同进程只用于调试、三进程用于部署的说明是否真实；Docker 仅暴露 User/Supplier 外部端口，Order HTTP 和内部 socket 是否留在私网。
13. 集成测试是否真实启动 HTTP、WebSocket、Redis 和三个独立进程，是否能在修复前稳定失败，是否存在 sleep/retry 刷绿或遗留进程。
14. 日志是否使用稳定事件和字段，是否泄露 Token、Authorization、请求体、地址快照或完整业务对象。
15. 公共 Go API、配置、JSON 与运行时行为变更是否经过兼容性和发布契约检查，新增能力是否被正确登记为兼容扩展。

## 建议复现命令

```bash
go test -race ./examples/06-shop-microservices/... -count=1

CORE_TEST_REDIS_ADDR=127.0.0.1:6379 SHOP_REDIS_ADDR=127.0.0.1:6379 \
go test ./examples/integration/06-shop-microservices -count=1 -timeout=15m

CORE_TEST_REDIS_ADDR=127.0.0.1:6379 SHOP_REDIS_ADDR=127.0.0.1:6379 \
go test ./examples/integration/06-shop-microservices-three-process -count=1 -timeout=15m

go vet ./examples/06-shop-microservices/... \
  ./examples/integration/06-shop-microservices \
  ./examples/integration/06-shop-microservices-three-process \
  ./pkg/server/cluster/... ./pkg/server/mq/... ./pkg/server/event/... ./pkg/server/router/...

./scripts/check-logging.sh
./scripts/test.sh release-contract
docker compose -f examples/06-shop-microservices/deploy/docker-compose.yml config
```

## 输出格式

- Findings：按 P0、P1、P2 排序，每项提供文件、行号、触发场景、实际影响和修复建议。
- 分别裁定：服务发现、跨服务调用、可靠事件、数据所有权、认证隔离、缓存/WebSocket、两种部署和测试真实性。
- 兼容性评估：Go API、HTTP 路由、JSON DTO、配置和运行时行为。
- 测试缺口与残余风险。
- 最终裁定：`APPROVED` 或 `CHANGES_REQUIRED`。
- 是否允许关闭示例 06 并进入下一个示例。
