# 商城微服务边界重构实施计划

> 状态：完成
> 日期：2026-07-17
> 范围：`examples/06-shop-microservices`、Core 可信内部调用方能力、集成测试、部署和现行文档

## 目标

按确认后的业务边界重建示例 06：

- Supplier 服务面向供应商用户，使用统一 Manage Hook 管理本人资料、商品和订单查询；只通过受限 Public 为其他服务提供供应商、商品数据。
- User 服务面向普通用户，使用 Manage 管理本人资料和地址，通过 Public facade 查询基础数据，通过 Private 下单、撤单、支付和查询订单。
- Order 服务是内部事实服务，Manage 仅供平台管理员，五个 Public 仅允许 User 服务调用；不提供 Private、WebSocket，也不向宿主机暴露 HTTP 端口。
- Core 增加冻结的内部调用方白名单：同进程信任 Source ServiceContext，跨进程信任已验证且与 `SourceService` 一致的 mTLS SAN，拒绝发生在 Parse 前。

## 已确认的业务规则

### 身份和数据

- 登录身份由 TestToken 模拟；User/Supplier 本地保存 `AuthUserID`，跨服务只使用数字业务 ID。
- 供应商和商品被订单引用后不能删除。
- 供应商只有管理员能禁用或重新启用；商品可由所属供应商或管理员上下架。
- 禁用用户和供应商仍可查看已有数据，但不能产生新的写操作。

### 订单和事件

- 下单必须提供客户端 `requestID`，Order 以 `{UserID}:{requestID}` 和请求指纹实现并发幂等。
- Order 创建成功时在同一事务写入可靠 `OrderCreated`，包含完整订单、供应商和商品快照。
- Supplier 消费订单事件后按 `OrderID` 幂等写永久 `SupplierOrder`；删除 Hook 只查询本地投影。
- 状态和支付变化发布 `OrderStatusChanged`、`PaymentChanged`；支付类型变化发布 `PaymentTypeChanged`。
- 消费方使用 Inbox/EventID 幂等，成功处理后才 ACK。

## 交付分解

### 任务 1：冻结内部调用方元数据

- [x] 增加 `router.WithInternalCallers(...)`。
- [x] RouterInfo 冻结时规范化、排序并复制白名单。
- [x] `GetInternalCallers()` 返回防御性副本。
- [x] 路由/OpenAPI 兼容快照记录 `x-internal-callers`。

提交：`6b5d680 feat(router): 增加内部调用方冻结元数据`

### 任务 2：执行前统一授权

- [x] 增加可信内部调用方请求/上下文契约。
- [x] 所有执行入口在 Parse、Validation、Do 前调用同一授权方法。
- [x] HTTP、缺失身份、错误服务和伪造身份 fail closed。
- [x] 同进程调用只信发起调用的 Source ServiceContext。

提交：`14387b2 feat(router): 在执行前校验可信内部调用方`

### 任务 3：mTLS 绑定远程身份

- [x] 从已验证 TLS peer 提取客户端证书身份。
- [x] 要求证书 SAN 等于载荷声明的 `SourceService`。
- [x] 无证书、错误 CA、错误 SAN 和声明不一致全部拒绝。
- [x] 仅验证成功后向 Router 注入可信身份。

提交：`4512c8b feat(grpc): 将内部调用方绑定到 mTLS 身份`

### 任务 4：统一共享契约

- [x] User、Supplier、Product、Order、Address ID 改为数字。
- [x] `AuthUserID` 只留在身份映射模型。
- [x] 订单、支付和基础资料事件携带 schemaVersion、EventID、Revision 和完整快照。
- [x] 定义稳定字符串 `PaymentID` 和支付尝试状态。

提交：`63b0406 refactor(example-06): 统一数字业务主键和事件快照`

### 任务 5：Supplier 持久化和永久投影

- [x] Supplier/Product 默认禁用，归属字段不可由请求篡改。
- [x] 可靠事件按 `OrderID` 幂等写 `SupplierOrder`。
- [x] 投影保留订单状态、支付状态、快照和最新 Revision。
- [x] Supplier/Product 删除保护只查本地投影。

提交：`bf680c0 refactor(example-06): 建立供应商订单永久投影`

### 任务 6：Supplier Manage 和受限 Public

- [x] Supplier/Product/Order 共用 Manage Hook 自动区分本人和管理员。
- [x] Supplier 删除仅管理员；Supplier 启停仅管理员。
- [x] Product CRUD 和启停允许所属供应商或管理员。
- [x] Order Manage 只保留 View/Search。
- [x] `GetSuppliers` 仅允许 User；`GetProducts` 允许 User/Order。
- [x] 删除 Supplier Private 和重复的 call API。

提交：

- `ba68eb0 refactor(example-06): 切换供应商 Manage 和内部 Public 边界`
- `123accc refactor(example-06): 用 Manage Hook 收紧供应商权限`

### 任务 7：Order 事实、幂等和状态机

- [x] 保存完整商品、供应商、地址、价格和数量快照。
- [x] `requestID` 唯一约束与请求指纹保证重试收敛。
- [x] 撤单不删除订单；已支付订单进入退款流程。
- [x] 支付尝试有稳定 `PaymentID`，处理中不重复创建。
- [x] 所有事实变化与 Outbox 同事务提交。

提交：`fe4b236 refactor(example-06): 建立可靠订单和支付事实`

### 任务 8：Order 内部 Public 和管理员 Manage

- [x] 五个 Public：CreateOrder、CancelOrder、CreatePayment、GetOrders、GetPaymentTypes。
- [x] 五个 Public 全部只允许 `shop-user`。
- [x] PaymentType 可管理；Order/PaymentRecord 只读加受控命令。
- [x] Order Manage 仅平台管理员。
- [x] Order 无 Private、WebSocket。

提交：`dc81d5c refactor(example-06): 将订单能力收口为内部 Public`

### 任务 9：User 和 Address Manage

- [x] User 使用数字 ID 和本地 `AuthUserID`。
- [x] User Manage 不物理删除；Address 支持本人 CRUD。
- [x] Search/Do Hook 自动限定本人，管理员可跨用户查看管理。
- [x] 禁用用户只允许查询。

提交：`12a105d refactor(example-06): 增加用户和地址 Manage Hook`

### 任务 10：User facade、Private 和 WebSocket

- [x] 三个 Public facade：供应商、商品、支付类型。
- [x] 四个 Private：下单、查询订单、撤单、支付。
- [x] 地址不再提供 Private CRUD。
- [x] User 只把可信数字 ID 传给 Order。
- [x] 只有 GetOrders 提供最终用户 WebSocket，并按 UserID 隔离。
- [x] 缓存通过可靠事件做定向失效。

提交：`59a5570 refactor(example-06): 重建买家 facade 和 Private 流程`

### 任务 11：同进程验收

- [x] 集成测试通过 Manage 创建供应商、商品、用户和地址。
- [x] 买家只经过 User Public/Private，不直接调用事实服务。
- [x] 验证 requestID、快照、撤单、支付、缓存和订单隔离。
- [x] 调用经过真实 ServiceContext/ServiceResolver。

提交：`bc1efa1 test(example-06): 验证同进程服务边界`

### 任务 12：三进程 mTLS 和部署

- [x] 三个独立进程通过 Redis 发现和 mTLS gRPC 调用。
- [x] 验证 User→Order、Order→Supplier gRPC 计数增长且 HTTP 为零。
- [x] 错误 PKI 无法建立健康调用。
- [x] Compose 仅暴露 User、Supplier；Order 无宿主机端口。
- [x] 证书挂载说明改为中文并明确 SAN/权限要求。

提交：`76774f7 test(example-06): 验证 mTLS 调用和隐藏订单服务`

### 任务 13：现行文档和能力沉淀

- [x] README 写明三类使用者、路由矩阵、Manage Hook、幂等、投影、缓存和事件。
- [x] RouterInfo/gRPC/框架/兼容/CI 文档登记可信内部调用方。
- [x] `use-digitalway-core` skill 和参考文档合并实现模式。
- [x] 增加文档契约测试，禁止恢复重复 call API。
- [x] 运行文档、API 和发布兼容门禁。

提交：`1e76168 docs(example-06): 记录可信内部服务边界`

### 任务 14：最终发布验证

- [x] 格式化所有本次修改的 Go 文件。
- [x] 运行示例 06 全包 race。
- [x] 运行同进程和三进程真实集成 race。
- [x] 运行 Core router/types/gRPC/compat 定向回归和 vet。
- [x] 运行日志、api-compat、release-contract 门禁。
- [x] 检查最终 diff，只提交本次范围，不覆盖用户已有修改。

最终验证中发现同进程全包 race 在 `TestUATBuyerOrderLifecycle` 中因 Supplier 本地 `supplier_order` 写入遇到 SQLite `database is locked` 失败；根因是前序 WebSocket 测试的订单投影后台写与后续 Manage 写共用同一 Supplier SQLite 文件，但部分写路径未进入统一事务互斥。修复为 Supplier 模型层写操作统一走 `RunTransaction`，用同一把本地互斥保护 Supplier 库写入。

提交：`83285e4 fix(example-06): 串行化供应商本地写入`

最终验证证据：

- `gofmt -w internal/compat/docs_contract_test.go examples/06-shop-microservices/supplier-service/models/models.go examples/06-shop-microservices/supplier-service/models/supplier_order.go`
- `GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./internal/compat -run TestCurrentDocsDescribeTrustedShopBoundaries -count=1`
- `GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test ./internal/compat -count=1`
- `GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./examples/06-shop-microservices/... -count=1`
- `GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./examples/integration/06-shop-microservices -count=1 -v`
- `GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./examples/integration/06-shop-microservices-three-process -count=1 -v`
- `GOCACHE=/private/tmp/core-codex-gocache rtk proxy go test -race ./pkg/server/types ./pkg/server/router ./pkg/server/transport/grpc ./internal/compat -count=1`
- `GOCACHE=/private/tmp/core-codex-gocache rtk proxy go vet ./examples/06-shop-microservices/... ./examples/integration/06-shop-microservices ./examples/integration/06-shop-microservices-three-process ./pkg/server/types ./pkg/server/router ./pkg/server/transport/grpc ./internal/compat`
- `rtk proxy ./scripts/check-logging.sh`
- `GOCACHE=/private/tmp/core-codex-gocache rtk proxy ./scripts/test.sh api-compat`
- `GOCACHE=/private/tmp/core-codex-gocache rtk proxy ./scripts/test.sh release-contract`
- `rtk docker compose -f examples/06-shop-microservices/deploy/docker-compose.yml config`

## 最终验收清单

- [x] Supplier 只有统一 Manage 和受限 Public，没有 Private、重复 call API。
- [x] User 有 User/Address Manage、三个 Public facade、四个买家 Private 和唯一订单 WebSocket。
- [x] Order 只有管理员 Manage 和五个 User-only Public，无 Private/WebSocket/宿主机 HTTP 端口。
- [x] 跨服务 ID 为数字，认证字符串只用于本地身份映射。
- [x] Supplier/Product 删除只查永久 `SupplierOrder`。
- [x] 四类事件通过事务 Outbox 发布，User/Supplier 通过 Inbox 幂等消费。
- [x] 受限路由在 Parse 前拒绝 HTTP、缺失信任、错误服务和 mTLS 身份不匹配。
- [x] 同进程与三进程都经过真实 Core Resolver。
- [x] 路由兼容快照包含内部调用方白名单。
- [x] 全量门禁通过并记录最终证据。
