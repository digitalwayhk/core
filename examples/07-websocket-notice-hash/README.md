# 07 - WebSocket Notice Hash

本示例展示跨节点 WebSocket 设计中的两个关键接口：

- `IWebSocketRouterNotice`：判断某条推送是否应该发送给当前订阅路由。
- `IRouterHashKey`：为订阅生成稳定 hash，框架可用 `routePath + hash` 查找本机客户端。

适用场景：

- funds 服务按 `userid` 分片。
- order 服务按 `accountTypeID` 分片。
- 推送时只希望命中某一组订阅，而不是广播所有客户端。

## 运行

```bash
go run ./examples/07-websocket-notice-hash/main -p 18087
```

## 订阅示例

```json
{"channel":"/api/account/watchbalance","event":"sub","data":{"userid":"u1001"}}
```

## 推送示例

```bash
curl -X POST http://127.0.0.1:18087/api/account/pushbalance \
  -H 'Content-Type: application/json' \
  -d '{"userid":"u1001","balance":"99.50"}'
```
