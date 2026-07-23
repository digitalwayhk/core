# 示例 06 P2 修复复审提示词

请只读复审示例 06 外部审查后的 P2 修复，不要修改代码。

## 审查范围

```bash
git diff 71c81bf..HEAD
```

## 原始 P2

1. `PaymentRecord.GetHash` 仅使用 OrderID，未来同订单多次支付尝试会冲突。
2. User/Supplier 的 `SubscribeExternalControl` 错误被静默忽略，缓存和 WebSocket 控制链路可能部分失效。
3. CHANGELOG 未登记 Redis Discovery、ServiceResolver、可靠 Streams 与示例 06。
4. Docker 构建镜像 Go 1.24 与仓库 Go 1.26 工具链不一致。
5. Compose 的 `-p`、DataCenterID 与实际业务端口关系不清晰。
6. 缺少从最终用户活动角度验证跨服务业务事实正确性的 UAT。

## 重点检查

1. 支付哈希是否同时包含订单、支付类型和支付尝试 ID；同一支付记录是否确定，同订单不同尝试是否不冲突。
2. `SubscribeExternalControls` 是否全有或全无；中途失败是否按逆序取消已建立订阅并返回包含失败 subject 的错误。
3. User/Supplier 启动遇到外部控制订阅失败时，是否记录稳定事件、包含 service/error 字段并 fail closed；是否泄露 subject payload 或认证信息。
4. 正常订阅是否把全部 cancel 纳入 Service.Stop，是否引入重复关闭、死锁或 goroutine 泄漏。
5. CHANGELOG 新增说明是否位于 Unreleased/Added，且没有覆盖工作区其他变更。
6. Dockerfile 是否与 `go.mod` 的 Go 版本一致；Compose 端口说明是否准确反映 `Port + DataCenterID - 1`。
7. `TestUATBuyerOrderLifecycle` 是否通过真实 HTTP、Redis、ServiceResolver 和 EventBridge，验证：
   - 下单商品与地址快照；
   - 商品改价后历史订单价格和总额不变；
   - 其他用户订单隔离；
   - 支付类型、流水金额和 PaymentID；
   - 买家与供应商视图最终支付状态一致；
   - 已支付订单撤销后双方状态一致。
8. UAT 是否使用确定性业务断言和 `Eventually` 等待可观测状态，而非固定 sleep 或放宽错误。
9. 本次修复是否改变公共 Go API、HTTP 路由、JSON DTO、配置或既有支付状态机语义。

## 建议命令

```bash
go test -race ./examples/06-shop-microservices/... -count=1

CORE_TEST_REDIS_ADDR=127.0.0.1:6379 SHOP_REDIS_ADDR=127.0.0.1:6379 \
go test ./examples/integration/06-shop-microservices -count=1 -timeout=15m

CORE_TEST_REDIS_ADDR=127.0.0.1:6379 SHOP_REDIS_ADDR=127.0.0.1:6379 \
go test ./examples/integration/06-shop-microservices-three-process -count=1 -timeout=15m

go vet ./examples/06-shop-microservices/... \
  ./examples/integration/06-shop-microservices \
  ./examples/integration/06-shop-microservices-three-process

./scripts/check-logging.sh
./scripts/test.sh release-contract
docker compose -f examples/06-shop-microservices/deploy/docker-compose.yml config
```

## 输出

- Findings 按 P0、P1、P2 排序，提供文件、行号、触发场景、影响和建议。
- 逐项判断六项原始 P2 是否关闭。
- 单独评价 UAT 的真实性、覆盖边界和剩余缺口。
- 最终裁定：`APPROVED` 或 `CHANGES_REQUIRED`。
- 是否允许关闭示例 06 P2 加固。
