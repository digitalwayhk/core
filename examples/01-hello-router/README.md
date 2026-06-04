# 01 - Hello Router

本示例展示最小的 Core 服务：

- 定义一个 `types.IRouter`。
- 使用 `router.DefaultRouterInfo` 自动生成 `/api/hello/ping` 路由。
- 通过 `run.NewWebServer()` 注册并启动服务。

## 运行

```bash
go run ./examples/01-hello-router/main -p 18081
```

## 测试

```bash
curl -X POST http://127.0.0.1:18081/api/hello/ping \
  -H 'Content-Type: application/json' \
  -d '{"name":"Vincent"}'
```

预期返回中会包含问候语和服务名。
