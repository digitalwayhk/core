# 06 - WebSocket Local

本示例展示本机 WebSocket 路由：

- 实现 `IWebSocketRouter` 记录订阅和取消订阅。
- HTTP 调用 `PublishPrice` 后，向订阅 `WatchPrice` 的本机客户端推送消息。

## 运行

```bash
go run ./examples/06-websocket-local/main -p 18086
```

## 订阅

连接 `/ws` 后发送：

```json
{"channel":"/api/market/watchprice","event":"sub","data":{"symbol":"BTCUSDT"}}
```

发布价格：

```bash
curl -X POST http://127.0.0.1:18086/api/market/publishprice \
  -H 'Content-Type: application/json' \
  -d '{"symbol":"BTCUSDT","price":"68000"}'
```
