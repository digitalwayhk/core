# 公共 API 兼容性表面

本文登记 `github.com/digitalwayhk/core` 对下游服务承诺维护的兼容表面。它描述边界和证据，不复制配置字段清单。配置字段的唯一权威来源是 [CONFIG_RUNTIME_CAPABILITY_MATRIX.md](CONFIG_RUNTIME_CAPABILITY_MATRIX.md)。

## 兼容级别

| 级别 | 含义 | 变更规则 |
| --- | --- | --- |
| Stable | 已被框架示例或下游服务采用 | 允许加性变更；删除、改签名或改语义必须经过任务 15 发布治理 |
| Deprecated | 为源码兼容保留，已有替代入口 | 保留至废弃登记指定的最早删除版本，并提供迁移测试 |
| Experimental | 尚未形成稳定承诺 | 变更必须写入 changelog，不能静默进入 Stable |
| Internal | 框架实现细节或测试 hook | 不纳入下游兼容承诺，外部项目不应引用 |

## Go API

| 包或入口 | 级别 | Owner | 消费场景 | 当前证据 |
| --- | --- | --- | --- | --- |
| `pkg/server/types.IRouter`、`IRequest`、`IResponse`、`RouterInfo` | Stable | server/router | public/private/manage 路由、服务间调用 | `pkg/server/types/router.go`、`interface.go`、examples |
| `pkg/server/config.ServerConfig` 及项目自有子配置 | Stable（按能力矩阵） | server/config | 服务配置、默认化和启动校验 | 任务 14 配置矩阵与 `config-contract` |
| `pkg/server/event` 的 EventBus、EventStream 与 Bridge 入口 | Stable（按能力矩阵） | server/event | 进程内事件与受支持 MQ 事件流 | event 单元测试与任务 14 生命周期测试 |
| `pkg/server/router.DefaultRouterInfo`、`NewRouterInfo` | Stable | server/router | 普通服务路由元数据 | `pkg/server/router/servicerouter.go`、`use-digitalway-core` skill |
| `pkg/server/router.NewServiceContext`、`NewServiceContextWithConfig` | Stable | server/router | 文件配置启动、程序化启动 | 任务 14 生产构造器与生命周期测试 |
| `pkg/server/types.ServerOption`、`IService` 和服务生命周期接口 | Stable | server/run | 服务注册、CORS、WebSocket、Start/Stop | `pkg/server/types/server.go`、run 生命周期测试 |
| `pkg/persistence/entity.Model`、`BaseModel`、`ModelList` | Stable | persistence | SQLite/MySQL/Badger 模型与查询 | persistence 单元/外部集成测试、examples/demo |
| `pkg/persistence/types` 的模型与数据库接口 | Stable | persistence | entity、ModelList 和数据库实现扩展 | persistence 编译与契约测试 |
| `service/manage.ManageService`、标准 CRUD、hook、`Operation` | Stable | service/manage | 管理后台 CRUD 与自定义操作 | `service/manage/crud_test.go`、examples/03/04/demo |
| Cluster、Transport、MQ 的已登记 factory/provider 入口 | Stable（按能力矩阵） | server runtime | 可插拔基础设施组装 | 任务 14 config-contract 与六包 race |
| `ManageService.Req`、`SetReq`、`IRequestSet` | Deprecated | service/manage | 旧代码读取共享请求状态 | `service/manage/manageservice.go`、请求隔离回归测试 |
| 进程级 `SetCrossNodeForwarder`、`GetCrossNodeForwarder` | Deprecated | server/types | 旧单服务跨节点转发 | `pkg/server/types/crossnode.go`；替代为 service-scoped API |
| `router.TestResult` 变量 | Deprecated | server/router | 旧 OpenAPI 测试结果注入 | `SetTestResult/GetTestResult` 为并发安全替代入口 |
| `pkg/utils` 导出辅助函数 | Experimental | utils | examples 和框架内部通用辅助 | 尚未完成逐符号稳定性登记；不得由包级存在推导全部 Stable |
| `pkg/server/transport/grpc/proto` 生成类型 | Experimental | server/transport | 内部 gRPC payload | 生成器与 wire 兼容基线待 15.3 登记 |
| 未导出符号、测试 helper、`internal/compat` | Internal | 对应包 | 实现与门禁 | 不允许下游直接依赖 |

## HTTP 与响应

| 表面 | 级别 | 契约 | 证据 |
| --- | --- | --- | --- |
| 普通 public/private 路由 | Stable | `/api/{service}/{router}`；private 要求认证 | `router.DefaultRouterInfo`、examples/01/demo |
| Manage CRUD | Stable | `/api/manage/{service}/{manage}/{operation}` | `service/manage.RouterInfo`、CRUD 测试 |
| ServerManage | Stable | `/api/servermanage/{router}`，注册时按服务重写 | server API 与 `TestToken` 文档 |
| 路由元数据 | Stable | method、path、pathType、auth、service | `internal/compat/testdata/routes.golden.json` |
| OpenAPI 结构 | Stable baseline | paths、method、schema、security；Host、端口和运行时 example 不属于契约 | `internal/compat/testdata/openapi.golden.json` |
| 默认成功/失败 JSON | Stable baseline | `traceid/errorCode/errorMessage/success/duration/data/host/showType` | `pkg/server/router.Response`；15.2 改造前不得改字段名或含义 |
| 自定义 `INewResponse` | Stable | 响应实例与 JSON 由服务拥有，框架只依赖 `IResponse` | `pkg/server/router.Request.NewResponse` |
| 类型化公共错误 | Stable | `ErrorKind` 决定 HTTP 状态、默认公共码与安全消息；支持 `%w`、`errors.Join` 和 `errors.Is/As` | `pkg/server/types/publicerror.go`、REST 表驱动测试 |
| 历史 `TypeError` 阶段码 | Stable compatibility | `NewTypeError` 签名和 600/700/800 保留；parse/validation→400，do→422，panic/未知→500 | `pkg/server/types/typeerror.go`、兼容测试 |
| 未分类普通错误 | Stable security | 固定返回 HTTP 500、`50000` 和 `internal server error`，不得按错误文字猜状态 | `pkg/server/trans/rest/error.go`、安全测试 |

当前 `run.GetOpenApi` 只输出 Public 与 Private 路由，因此 OpenAPI golden 的稳定范围也仅限这两类。Manage 与 ServerManage 仍属于稳定 HTTP 路由，但本阶段由路由元数据、Manage CRUD 测试和 server API 测试保护；是否纳入 OpenAPI 由后续兼容变更单独评估，不能从当前 golden 推导其已被覆盖。

默认 `router.Response` 在 HTTP 边界通过 `ISetPublicError` 写入稳定公共码和安全消息，响应字段名保持不变。服务自定义 `INewResponse` 仍由服务拥有；若希望框架写入标准安全错误，应同时实现 `ISetPublicError`，否则其序列化与脱敏责任由该服务承担。

## 配置

- 项目自有 Server/Auth、Cluster、Transport、MQ 字段、默认值、owner、支持状态和运行时证据仅维护在 `docs/codex/CONFIG_RUNTIME_CAPABILITY_MATRIX.md`。
- `supported` 值属于兼容表面；`rejected` 值只承诺在启动前返回可操作错误；`upstream` 字段遵循 go-zero 对应版本的契约。
- `Cluster.Mode=off` 和 `MQ.Mode=off` 的旧字段保留规则属于迁移兼容，不代表关闭字段已获得运行时支持。
- 配置 struct 或 tag 变化必须同时通过 `./scripts/test.sh config-contract`，不能在本文复制另一份字段表规避闭集门禁。

## 数据与生命周期

| 表面 | 级别 | 兼容要求 | 证据 |
| --- | --- | --- | --- |
| `Model.NewModel` 初始化 | Stable | 嵌入模型必须可由 `ModelList.NewItem` 初始化 | entity/manage 测试与 skill |
| `BaseModel.Code/GetHash` | Stable | Code/hash 语义不能无迁移改变 | persistence/manage 兼容测试 |
| Manage hook 顺序与 stop 语义 | Stable | Before/After、stop/result/error 顺序保持 | `service/manage/crud_test.go` |
| ServiceContext 运行资源关闭 | Stable | owner 有界关闭、重复关闭幂等、关闭后不承诺复用 | 任务 12/14 生命周期与 race 测试 |
| Provider 注册扩展点 | Stable（已登记部分） | 注册、注销、并发与关闭语义保持 | cluster/transport/mq factory 测试 |

## 快照规则

- 路由和 OpenAPI 快照由生产 `RouterInfo`、`ServiceRouter` 和 `run.GetOpenApi` 生成，不维护手写的第二套路由表。
- 普通测试只读 golden，缺失或漂移立即失败；仅显式设置 `UPDATE_GOLDEN=1` 才会更新测试基线。
- 更新 golden 必须与对应公共变化、迁移说明和外部审查放在同一小节提交中。
- 规范化仅删除 Host、端口、trace ID、duration 等运行时噪声；method、path、auth/security、请求/响应 schema 变化必须保留为 diff。

## 导出 Go API 门禁

- `api/public-packages.txt` 是当前受保护公共包的唯一清单；`api/public-api.txt` 记录工具版本和包到基线文件的确定映射。
- `api/public-api/*.apidiff` 由 Go 官方维护的 `golang.org/x/exp/cmd/apidiff` 生成，版本固定在独立 `tools/go.mod`，不进入根运行时依赖。
- `scripts/check-public-api.sh` 只读比较当前工作树：兼容新增会报告但允许通过；删除导出符号、改变签名、收紧接口等不兼容变化返回非零。
- `scripts/update-public-api.sh` 是唯一更新入口。普通测试不会覆盖基线；更新必须与 changelog、迁移说明和审查一起提交。
- `apidiff` 保护编译期 Go API 兼容性，但官方文档明确它是近似判断，不能发现行为变化。HTTP/JSON/路由由 `api-compat` 保护，配置由 `config-contract` 保护，生命周期和安全仍需对应行为测试。

## 验证

```bash
./scripts/test.sh api-compat
./scripts/test.sh public-api
./scripts/test.sh config-contract
```

废弃版本和消费方精确版本将在任务 15.4 完成；在对应小节完成前不提前声明发布门禁已生效。
