# 商城模型与 Manage 继承示例

`03-shop-inheritance` 是独立完整应用，演示如何在 Digitalway Core 服务中为模型和 Manage 建立职责清晰的继承层，同时保留商品、订单、支付和 WebSocket 业务闭环。

## 继承结构

```text
entity.Model                 manage.ManageService[T]
└── ShopModel                └── ShopManage[T]
    ├── BaseDataModel            ├── BaseDataManage[T]
    │   ├── Product              │   ├── ProductManage
    │   ├── Supplier             │   ├── SupplierManage
    │   └── PaymentType          │   └── PaymentTypeManage
    └── BusinessModel            └── BusinessManage[T]
        ├── Order                    ├── OrderManage
        └── PaymentRecord            └── PaymentRecordManage
```

每层 Manage 都接收最终具体 owner。具体 hook 先显式调用父级 hook，再追加自己的业务规则；Go 嵌入本身不提供虚方法分派。

## 业务能力

- 商品、供应商和支付类型共享 Code、Name、Enabled、Description。
- 基础资料新增后默认禁用，只能通过继承得到的启用、禁用命令改变状态。
- 供应商管理页展示只读商品子表，商品写入统一经过 ProductManage。
- 禁用供应商不修改商品状态，但商品立即从 Public 查询消失且不能下单。
- 订单保存商品、供应商和价格快照。
- 订单与支付流水共享业务模型状态能力，具体类型负责强类型转换和中文显示。
- 支付失败、重试、确认、退款和用户隔离 WebSocket 与第二个示例保持一致。

## 分层

```text
api/public|private|manage -> business -> models -> IDataAction
```

Public/Private 返回独立 DTO。SQLite 只在 models 的数据访问组合根选择，事务通过克隆的 `IDataAction` 隔离。

## 运行

```bash
go run ./examples/03-shop-inheritance/main -view 0
```

首次运行由框架自动生成 `server.json` 和 `inheritanceshop.json`，示例不提交运行时配置。

## 测试

```bash
go test -race ./examples/03-shop-inheritance/... -count=1
go test -race ./examples/integration/03-shop-inheritance -count=1 -timeout=15m
go vet ./examples/03-shop-inheritance/... ./examples/integration/03-shop-inheritance
```

完整设计见 `docs/superpowers/specs/2026-07-14-shop-inheritance-example-design.md`。
