# 支付商城示例

`02-shop-payment` 是在最简商城之上的完整进阶示例，演示 API、业务层和模型持久化层分离，以及支付结果滞后时的订单与支付流水状态机。

## 包含能力

- 商品 Manage CRUD，以及已被订单引用时禁止删除；
- 支付类型 Manage CRUD、启用和禁用，以及已被流水引用时禁止删除；
- 订单只读 Manage 和中文状态字段；
- 支付流水只读 Manage，以及确认支付、支付失败、确认退款命令；
- Public 商品与启用支付类型查询；
- Private 下单、本人订单、删除未支付订单、发起支付和申请撤销；
- 订单变化 WebSocket，按认证用户隔离；
- 支付失败重试、支付确认和异步退款状态流转；
- 使用 TestToken、真实进程、SQLite、HTTP 和 WebSocket 的集成测试。

## 分层

```text
api/public|private|manage -> business -> models -> IDataAction
```

API 不直接操作数据库，business 统一处理所有权、引用保护、金额计算和跨模型事务，models 只负责实体与持久化。

## 运行

```bash
go run ./examples/02-shop-payment/main -view 0
```

首次运行由框架自动生成配置，不需要在示例中提交运行配置文件。

## 测试

```bash
go test ./examples/02-shop-payment/... -count=1
go test -race ./examples/02-shop-payment/... -count=1
go test ./examples/integration/02-shop-payment -count=1 -timeout=15m
```

完整设计见 `docs/superpowers/specs/2026-07-14-shop-payment-example-design.md`。
