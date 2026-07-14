# 最简商城完整示例

本示例是 Digitalway Core 的第一入口。它包含商品和订单两个模型，并使用框架现有能力完成管理 CRUD、公开查询、用户鉴权、SQLite 持久化与 WebSocket 订单通知。

## 启动

首次运行会在可执行文件所在目录自动创建 `etc/server.json` 和 `etc/shop.json`，无需手工准备运行配置：

```bash
cd examples/01-simple-shop/main
go build -o simple-shop .
./simple-shop -view 0
```

默认商城地址为 `http://127.0.0.1:8081`。演示完成后可以删除本地生成的 `simple-shop`、`models.ldb`、`models.ldb-wal` 和 `models.ldb-shm`。

## 获取令牌

```text
GET http://127.0.0.1:8081/api/servermanage/testtoken?userid=user-a
GET http://127.0.0.1:8081/api/servermanage/testtoken?userid=admin&type=1
```

- 默认或 `type=0`：普通用户令牌。
- `type=1`：Manage 管理员令牌。

HTTP 鉴权请求使用 `Authorization: Bearer <token>`。

## 接口

| 类型 | 路径 | 说明 |
| --- | --- | --- |
| Manage | `/api/manage/shop/productmanage/{view,search,add,edit,remove}` | 商品管理 |
| Manage | `/api/manage/shop/ordermanage/{view,search}` | 只读订单管理 |
| Public | `GET /api/shop/getproducts?id=&name=` | 商品 ID 精确、名称模糊组合筛选 |
| Private | `POST /api/shop/addorder` | 按商品 ID 和数量下单 |
| Private | `GET /api/shop/getorders` | 查询本人订单 |
| Private | `POST /api/shop/deleteorder` | 物理删除本人订单 |

下单请求只提交 `productID` 与 `quantity`。订单保存商品 ID、名称和单价快照，UserID 只从令牌读取。

## WebSocket

连接 `ws://127.0.0.1:8081/ws` 后先登录：

```json
{"event":"sub","channel":"logon","data":{"token":"<普通用户令牌>"}}
```

登录成功后订阅本人订单：

```json
{"event":"sub","channel":"/api/shop/getorders","data":{}}
```

新增和删除订单会向当前用户发送扁平的订单 DTO，其 `action` 为 `created` 或 `deleted`。WebSocket 只面向最终外部用户，内部服务通信使用 TransportSelector 与 EventBridge。

## 集成测试

测试会在系统临时目录启动独立子进程，由框架首次运行自动生成配置和 SQLite 数据，使用真实 HTTP、TestToken 与 WebSocket，结束后自动清理：

```bash
go test ./examples/integration/01-simple-shop -count=1
go test ./examples/integration/01-simple-shop -count=10
go test -race ./examples/integration/01-simple-shop -count=1
```
