# 05 - OpenAPI Response

本示例展示 `types.IRouterResponse`：

- 路由实现 `GetResponse()` 后，OpenAPI 生成器可读取响应结构。
- 适合 public/private API，而不是只服务管理端页面。

## 运行

```bash
go run ./examples/05-openapi-response/main -p 18085
```

启动后可访问服务提供的 OpenAPI/Swagger 页面，查看 `CreateInvoice` 的请求和响应结构。
