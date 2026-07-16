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
