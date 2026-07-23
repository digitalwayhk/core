# Codex 修复提示（短）

只读审核报告：`docs/codex/SHOP_PAYMENT_EXAMPLE_REVIEW.md`  
规格：`docs/superpowers/specs/2026-07-14-shop-payment-example-design.md` §6  

## 任务

修复 **P1**：`examples/02-shop-payment` 单例 `IDataAction` 事务状态与并发非事务写隔离。

- 重点文件：`examples/02-shop-payment/models/data_action.go` 及 models 持久化路径  
- 所有 `getDataAction()` 访问须与 `RunInTransaction` 一致地避免共享 `isTansaction`/`tx` 被旁路写入（全局串行或事务独立适配器）  
- 增加混合路径 `-race` 测试：事务进行中并发 `CreateOrder`/Insert，断言不串事务、race 绿  
- 保留现有 `payment_flow_test` 通过  
- 不必改框架全局，优先示例内最小修复；P2 可选  

## 验证

```bash
go test -race ./examples/02-shop-payment/... -count=1
go test -race ./examples/integration/02-shop-payment -count=1 -timeout=15m
go vet ./examples/02-shop-payment/... ./examples/integration/02-shop-payment
./scripts/check-logging.sh
```
