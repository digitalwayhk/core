# 02-shop-payment 审核结果（2026-07-14）

- 范围：`git diff 382dbf8..58dd1f2`（`cd1cfc1` + `58dd1f2`）
- 规格：`docs/superpowers/specs/2026-07-14-shop-payment-example-design.md`
- 路径：`examples/02-shop-payment/**`、`examples/integration/02-shop-payment/**`
- 裁定：**CHANGES_REQUIRED**（有 P1，**不能**进下一个示例）

## 测试

- `go test -race ./examples/02-shop-payment/... -count=1` → PASS  
- `go test -race ./examples/integration/02-shop-payment -count=1 -timeout=15m` → PASS  
- `go vet` + `./scripts/check-logging.sh` → PASS  

## Findings

### P1（阻塞）共享 IDataAction 事务状态泄漏

- 文件：`examples/02-shop-payment/models/data_action.go`（`RunInTransaction` / `transactionMu`）
- 现象：`transactionMu` 只包住 `RunInTransaction`；`CreateOrder`/`Delete`/其它 `getDataAction()` 写路径不持锁，却共用 `GetGlobalSqliteInstance`。
- 底层：`pkg/persistence/database/oltp/sqlite.go` 在 `isTansaction==true` 时把 Insert/Update 打进共享 `tx`。
- 风险：并发非事务写可能并入他人事务，或对 `isTansaction`/`tx` data race；与规格 §6 不符。
- 现有并发测只压 `CreatePayment`（都在事务内），测不到混合路径。
- 修复：所有 `getDataAction()` 访问统一串行；或事务用独立适配器。补「事务中 + 非事务写」`-race` 测试。

### P2（不阻塞）

1. 支付类型 Name 唯一仅内存校验，无 DB 唯一（`paymenttype` GetHash 只有 Code）。
2. 删除失败订单不级联流水（可文档化或级联）。
3. 集成缺口：跨用户付/撤/删；WS `payment_failed`/重试；Edit `Enabled` 拒绝；PaymentRecord 无 Add/Edit/Remove 的 404。
4. 幂等确认支付不校验订单侧状态是否一致（`business/payment.go` finishPending）。

## 已通过（摘要）

- 分层 API→business→models→IDataAction 正确，models 无反向依赖。
- 状态机主体正确；Private 只信 `req.GetUser()`；金额快照；提交后才 WS，按用户过滤。
- Manage 命令/引用保护/只读 Order·PaymentRecord 方向正确。

## 修复优先级

1. 关 P1 + 混合并发 race 测试  
2. 可选 P2  
3. 复审目标：APPROVED 且允许下一示例  
