# Casdoor 双域认证与商城权限示例

`05-shop-casdoor-rbac` 在示例 03 的模型、Manage 继承、订单、支付和 WebSocket 业务上，演示完整的 Casdoor 登录与双认证域权限隔离。本目录是独立应用，不需要引用示例 03。

## 权限矩阵

| 能力 | 匿名 | 普通用户 Auth Token | 管理员 Manage Token |
| --- | --- | --- | --- |
| 查询商品、供应商、支付类型 | 允许 | 允许 | 允许 |
| 下单、本人订单、支付、删除或撤销 | 拒绝 | 允许 | 拒绝 |
| 基础数据 CRUD 与启禁用 | 拒绝 | 拒绝 | 允许 |
| 订单、支付流水、身份事件后台查询 | 拒绝 | 拒绝 | 允许 |
| 确认支付等受控后台命令 | 拒绝 | 拒绝 | 允许 |

Auth 与 Manage 必须使用不同的 Casdoor Client、Access Secret、Refresh Secret 和 Webhook Secret。Auth Token 不能访问 Manage API，Manage Token 也不能访问 Private API。

## 三个认证 Hook

`ShopService` 直接展示三个 Hook 的不同职责：

- `OnAuth`：在签发 Access Token 前，按已验证 `AuthType` 注入 `role` 和 `shop_scope`。角色不接受客户端传入。
- `OnAuthRequest`：在验签和撤销校验后、Router 前，同时核对认证域、角色、scope 和路由类型。
- `OnCasdoorEvent`：在撤销事实已持久化后，幂等写入只读身份审计表。审计记录不保存 Token、Header、Claims 或原始 Webhook。

`IdentityEventManage` 只暴露 `View` 和 `Search`，不允许通用 Add、Edit 或 Remove 绕过事件 Hook。

## Manage 服务级钩子

`ShopManage` 不承担具体模型的解析和校验，而是统一处理整个商城后台的横切行为：

- `DoBefore`：统一分派标准操作，并在这一总入口记录所有前置 Hook 失败。
- `DoAfter`：统一分派成功后操作，并在这一总出口记录后置 Hook 失败或最终成功。动作级 `On...After` 不重复打日志。每条日志都包含最终 `owner` 类型名，可在同一服务内按 ProductManage、OrderManage 等管理模块统一聚合。
- `SearchBefore`：把全服务查询限制在每页 100 条，无显式排序时使用 ID 倒序。
- `SearchAfter`：保留框架默认项行为，并把查询 `Tag` 透传回前端。

最终 Manage 通过嵌入 `ShopManage` 自动获得全部默认 Hook，只需重写关心的 `OnViewBefore`、`OnAddBefore`、`OnEditBefore`、`OnRemoveBefore`、`OnSearchBefore` 或对应 `After` 方法。每个方法都代表该服务下所有派生 Manage 的同类命令，因此可以把通用授权、审计、缓存失效和查询约束留在服务层，再把模型类别和具体业务规则逐层下沉。

派生 Manage 有三种使用方式：不实现方法时完全继承父级；同名方法不调父级时完全替换；先调父级再追加逻辑时保留公共能力并增加条件。当前示例的层次为：

- `ShopManage`：管理身份、生命周期集中日志、100 条分页上限和默认排序。
- `BaseDataManage`：新增默认禁用、禁止直接修改启停状态，以及统一调用模型新增、修改、删除校验。基础数据模型在 `RemoveValid` 中检查业务引用，有引用时只能禁用。
- `BusinessManage`：阻止通用 CRUD 绕过状态机，并将业务数据分页收紧为 50 条。
- 最终 Manage：`ProductManage` 只追加供应商可用性，`SupplierManage` 完全继承基础数据 CRUD，`PaymentTypeManage` 只追加已使用编码稳定性；三者的删除引用保护都由模型 `RemoveValid` 统一进入。Order/PaymentRecord/IdentityEvent 再按查询成本分别收紧到 30/30/25 条。

`IdentityEventManage.OnSearchBefore` 明确先调用 `BusinessManage.OnSearchBefore`，再增加审计查询条件，展示了多层规则累加。基础模型的 `AddValid/UpdateValid` 已经包含字段规范化，因此本示例不再重复实现 `ParseAfter`。

`OrderManage.ViewModel` 只设置页面属性；`PaymentStatus` 由模型反射生成，再统一由 `ViewFieldModel` 设置标题、搜索和状态选项，避免重复字段。

## 目录与依赖方向

本示例把模型、业务和 Manage 都按领域分包，用目录表达继承层次，而不是把所有文件放在同一个包中：

```text
models/
  common/                 # ShopModel、BaseDataModel、BusinessModel 和公开错误
  basedata/               # 商品、供应商、支付类型
  transaction/            # 订单、支付流水和状态
  identity/               # Casdoor 身份事件审计
  internal/store/         # 示例私有的 IDataAction 和事务边界
  schema/                 # 全部模型建表组装点
business/
  basedata/               # 基础资料规则
  transaction/            # 订单与支付状态机、跨模型事务
  identity/               # 身份事件幂等审计
api/manage/
  common/                 # 全 Shop 服务共享的 Manage Hook 和集中日志
  basedata/               # 基础资料 CRUD、启用/禁用命令
  transaction/            # 业务数据查询和受控状态命令
  audit/                  # 只读身份审计
```

`models`、`business` 和 `api/manage` 根包只保留兼容门面，使旧的构造函数和路由注册保持稳定。新实现应直接放入对应子包；依赖只能从 API 指向 business、再指向 models，不得反向引用。

单元测试与被测实现同目录，例如 `business/transaction/payment_flow_test.go` 和 `api/manage/audit/identityeventmanage_test.go`。跨子包的继承或兼容门面契约测试才保留在根包；真实进程、HTTP、Casdoor 和 WebSocket 测试只放在 `examples/integration/05-shop-casdoor-rbac`。固定样本数据应使用 Go 约定的 `testdata/`，不另建通用 `test` 包。

## 运行

首次启动：

```bash
go run ./examples/05-shop-casdoor-rbac/main -view 0
```

框架会先自动生成 `etc/server.json` 和 `etc/casdoorrbacshop.json`。停止服务后，在 `casdoorrbacshop.json` 中分别配置：

- `Auth.AccessSecret` / `Auth.RefreshSecret` / `Auth.CasDoor`：普通用户域。
- `ManageAuth.AccessSecret` / `ManageAuth.RefreshSecret` / `ManageAuth.CasDoor`：管理员域。
- `AuthRevocation.Mode=local` 和应用专属 `BadgerPath`：单实例撤销权威。

两个 Casdoor YAML 都使用以下结构，但字段值必须独立：

```yaml
certificate: |-
  -----BEGIN PUBLIC KEY-----
  ...
  -----END PUBLIC KEY-----
server:
  endpoint: https://casdoor.example.com
  client_id: shop-auth-or-manage-client
  client_secret: replace-with-domain-client-secret
  organization: shop-auth-or-manage-org
  application: shop-auth-or-manage-app
  frontend_url: https://casdoor.example.com
```

前端先请求 `/api/casdoor?type=auth|manage` 获取域配置，然后将 OAuth code 传给 `/api/casdoor/callback`。Webhook 固定使用 `/api/casdoor/webhook?type=auth|manage` 和对应域的 Bearer Secret。

## 集成测试

`examples/integration/05-shop-casdoor-rbac` 启动真实示例进程和本地 Fake Casdoor，完整经过：

- `/api/casdoor` 域配置、OAuth callback、Access/Refresh Token。
- Public、Private、Manage 全部商城 API 和订单 WebSocket。
- Auth/Manage 串域拒绝、客户端角色伪造拒绝。
- logout Webhook、旧 Token 失效和幂等身份审计。

```bash
go test -race ./examples/05-shop-casdoor-rbac/... -count=1
go test -race ./examples/integration/05-shop-casdoor-rbac -count=1 -timeout=15m
go vet ./examples/05-shop-casdoor-rbac/... ./examples/integration/05-shop-casdoor-rbac
```

完整设计见 `docs/superpowers/specs/2026-07-16-shop-casdoor-rbac-example-design.md`。
