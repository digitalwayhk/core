# 示例 3 商城继承实现外部审查提示词

请对第三个商城继承示例执行一次**只读实现审查**，不要修改代码、不要提交文件。

## 审查范围

```text
基线提交：33223eb docs: 设计商城继承示例
实现提交：418b3fe feat: 添加商城继承完整示例
审查差异：git diff 33223eb..418b3fe
```

设计规格：

```text
docs/superpowers/specs/2026-07-14-shop-inheritance-example-design.md
```

主要实现：

```text
examples/03-shop-inheritance
examples/integration/03-shop-inheritance
examples/README.md
```

## 审查目标

确认实现是否完整、真实地演示了模型继承、Manage 继承、供应商业务、支付状态机和真实进程集成测试，并确认没有为了示例修改或绕过框架生产契约。

## 重点检查

### 1. 模型继承

检查以下继承链是否完整初始化，是否存在 nil 嵌入、字段遮蔽、重复状态字段或反射异常：

```text
entity.Model
└── ShopModel
    ├── BaseDataModel
    │   ├── Product
    │   ├── Supplier
    │   └── PaymentType
    └── BusinessModel
        ├── Order
        └── PaymentRecord
```

重点确认：

1. 每个具体模型的构造方法和 `NewModel()` 都初始化完整链。
2. `BaseDataModel` 的 Code、Name、Enabled、Description 契约一致。
3. Code 规范化为小写，Code/Name 同表唯一，业务预检与数据库约束均有效。
4. `BusinessModel.Status int` 由具体模型转换为强类型状态，业务代码没有散落未命名整数。
5. 订单保存商品、供应商和价格快照，基础资料变化不影响历史订单。
6. `Supplier.Products` 外键正确，不会导致错误迁移、递归保存或越权写入。

### 2. Manage 继承与 hook 分派

检查以下继承链：

```text
manage.ManageService[T]
└── ShopManage[T]
    ├── BaseDataManage[T]
    │   ├── ProductManage
    │   ├── SupplierManage
    │   └── PaymentTypeManage
    └── BusinessManage[T]
        ├── OrderManage
        └── PaymentRecordManage
```

重点确认：

1. 最终具体 owner 是否传入所有父层和 Router 实例。
2. 子级重载 hook 是否先显式调用直接父级，通用规则没有被覆盖丢失。
3. Manage 长期实例是否完全不保存请求、用户、选中行或临时命令状态。
4. 基础资料 Add 是否无条件强制禁用。
5. 普通 Edit 是否无法直接改变 Enabled，启停只能经过通用命令。
6. 请求级 `EnableBaseData`、`DisableBaseData` 是否能从 Router 包装器恢复最终 owner，不会误绑定父层或共享请求状态。
7. Product 启用时是否重新校验供应商存在且启用。
8. Supplier.Products 子表是否真实可查询，同时 Add/Edit/Remove 均关闭。
9. OrderManage 是否只有 View/Search；PaymentRecordManage 是否只有 View/Search 和三个受控状态命令。

### 3. 供应商与商品规则

确认：

1. 商品新增、编辑、启用都要求供应商存在且启用。
2. 禁用供应商不修改商品自身 Enabled。
3. 禁用供应商后，Public 商品查询和新下单立即失败。
4. 重新启用供应商后，原本启用的商品恢复可见、可下单。
5. 供应商有商品时不能删除。
6. 商品被历史订单引用后不能删除。
7. Public 商品 DTO 包含供应商 ID、Code、Name，但不暴露持久化内部字段。
8. Public 筛选参数为空、单独使用和组合使用时语义正确。

### 4. 支付、事务与幂等

确认示例 2 的能力没有在继承改造后退化：

1. 支付失败后可创建新尝试，历史流水保留。
2. 确认支付、标记失败、申请撤销、确认退款都在独立克隆的 `IDataAction` 事务中完成。
3. 状态命令在事务内重新读取事实，不信任客户端或 Manage 旧状态。
4. 重复确认支付和退款保持幂等。
5. 并发创建支付只有一个成功。
6. 事务回滚不会捕获或回滚并发普通订单写入。
7. 禁用支付类型只阻止新支付，不阻止既有流水完成状态迁移。

### 5. 认证、DTO、WebSocket 与事件

确认：

1. Private API 只使用 `req.GetUser()`，不信任请求 UserID。
2. 用户只能查询、删除、支付和撤销自己的订单。
3. Public/Private 返回独立 DTO，不直接返回持久化模型。
4. `GetOrders` WebSocket 订阅实例按会话持久存在，不进入普通请求池。
5. WebSocket 只向订单所属用户推送，其他用户不会收到事件。
6. HTTP 查询与 WebSocket 共用订单 DTO，只有通知设置 Action。
7. 通知在事务提交后发送，失败不回滚业务事务。
8. WebSocket 只用于最终外部用户，未被用于内部服务通信。

### 6. 集成测试真实性

检查 `examples/integration/03-shop-inheritance` 是否：

1. 复用 `examples/integration/helpers.go`。
2. 启动真实服务进程并使用框架自动生成配置。
3. 使用系统临时目录、真实 SQLite、HTTP、TestToken 和 WebSocket。
4. 保留 `TestManageAPIs`、`TestPublicAPIs`、`TestPrivateAPIs` 三个整组入口。
5. 每个 API 或 command 都有独立子测试。
6. 覆盖继承启停、供应商只读子表、联合有效性、订单快照、支付状态机、用户隔离和清理。
7. 没有通过 sleep、retry、忽略错误或弱断言制造假绿。
8. TestMain 在失败和成功路径都能关闭进程并清理临时目录。

### 7. 范围与兼容性

确认提交没有修改 `pkg/`、`service/manage` 等框架生产代码，没有新增公共框架 API、配置字段或隐式运行依赖。检查是否误提交数据库、配置、日志、二进制文件，或错误引用示例 1、2 的业务包。

## 必跑命令

```bash
git diff --check 33223eb..418b3fe
go test -race ./examples/03-shop-inheritance/... -count=1
go test -race ./examples/integration/03-shop-inheritance -count=1 -timeout=15m
go vet ./examples/03-shop-inheritance/... ./examples/integration/03-shop-inheritance
./scripts/check-logging.sh
```

集成测试需要本地端口权限，但不需要 Docker、Redis 或外部 MQ。

## 输出格式

请严格输出：

1. `Findings`：按 P0、P1、P2 排序，每项包含文件、行号、触发场景、影响和修复建议。
2. `规格符合性`：逐项说明模型继承、Manage 继承、供应商规则、支付状态机、WebSocket 和集成测试是否达标。
3. `兼容性评估`：说明是否修改公共 Go API、HTTP、JSON、配置或运行时语义。
4. `测试真实性与缺口`：说明哪些测试能在修复前失败，是否存在假绿或遗漏。
5. `命令结果`：列出实际执行命令、退出码和关键结果。
6. `最终裁定`：只能是 `APPROVED` 或 `CHANGES_REQUIRED`。
7. `是否允许关闭示例 3 并进入下一个示例`：明确回答“是”或“否”。

即使没有 P0/P1，也请列出仍值得后续处理的 P2；不要因为这是示例而降低并发、权限、事务和测试真实性标准。
