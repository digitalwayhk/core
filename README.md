# Digitalway Core

Digitalway Core 是构建 Go 商业服务的应用组装框架。它以 go-zero、GORM 和成熟基础设施客户端为底座，提供统一的路由、模型、管理 CRUD、服务生命周期、集群、传输、MQ、事件桥接和 WebSocket 约定。框架只保留 Digitalway 领域契约，不重复实现通用客户端、日志或并发原语。

## 环境

- Go 版本以 [go.mod](./go.mod) 为准。
- 核心 HTTP、配置、日志和生命周期复用 go-zero。
- SQL 模型契约使用 GORM。
- 外部依赖测试默认跳过，通过显式环境变量或 Docker Compose 模式启用。

```bash
go get github.com/digitalwayhk/core@latest
```

## 最小服务

路由实现 `types.IRouter` 的 `Parse`、`Validation`、`Do` 和 `RouterInfo`。普通 public/private 路径为：

```text
/api/{service}/{structLower}
```

`api/public` 无需认证；`api/private` 自动要求认证，并通过 `req.GetUser()` 读取身份。完整代码见 [最简商城示例](./examples/01-simple-shop)。

```go
func (own *Ping) Parse(req types.IRequest) error { return req.Bind(own) }
func (own *Ping) Validation(req types.IRequest) error { return nil }
func (own *Ping) Do(req types.IRequest) (interface{}, error) {
	return map[string]string{"message": "pong"}, nil
}
func (own *Ping) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfo(own)
}
```

## 模型与管理 CRUD

- 使用 `entity.NewModelList[T](nil)` 访问模型。
- 嵌入 `*entity.Model` 或 `*entity.BaseModel` 的类型必须在 `NewModel()` 中初始化嵌入字段。
- 只有天然具有稳定 `Code` 语义的资料模型才使用 `BaseModel`。
- 管理 CRUD 路径为 `/api/manage/{service}/{manageStructLower}/{operationLower}`。

模型、Manage CRUD 和私有订单接口见 [最简商城示例](./examples/01-simple-shop)。

## 安全与配置

- CORS 开启时必须显式配置 origin；`*` 仅在调用方主动选择时允许。
- `TrustedProxies` 默认空，忽略 `X-Forwarded-For`/`X-Real-IP`；反向代理部署必须配置可信 IP/CIDR。
- 配置文件和迁移结果使用 `0600` 权限；不支持的能力在启动前 fail closed。
- 运行时日志统一使用 go-zero `logx`，不得记录 token、TOTP、完整请求/响应、payload、SQL 或参数值。

完整日志级别、字段和错误归属规则见 [日志审计与规范](./docs/codex/LOGGING_AUDIT_AND_STANDARD.md)。

## 集群、传输与 MQ

能力是否可用由配置校验、运行时 factory 和行为测试共同决定，不能仅依据配置字段存在。当前支持范围和外部依赖接入方式见 [配置到运行时能力矩阵](./docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md)。

## 验证

```bash
./scripts/test.sh quick
./scripts/test.sh server
./scripts/test.sh release-contract
./scripts/test.sh integration-external-docker
```

更完整的场景选择、成熟度和测试命令见 [框架场景使用指南](./docs/codex/FRAMEWORK_USAGE_GUIDE.md)。
