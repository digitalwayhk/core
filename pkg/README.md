# pkg 目录说明

`pkg` 是 core 框架的基础能力目录。当前项目中需要重点维护和阅读的包是 `json`、`persistence`、`server`、`utils`；`dec`、`fileserver`、`localization` 当前暂不作为有效功能目录分析。

## 有用目录

### json

`pkg/json` 是 JSON 序列化封装层，当前使用 `json-iterator/go` 的 `ConfigCompatibleWithStandardLibrary` 配置，提供 `Marshal`、`Unmarshal`、`MarshalToString`、`UnmarshalFromString`。它的定位是替代标准库 `encoding/json`，在保持兼容性的基础上优化序列化性能。

后续优化 JSON 序列化时，优先在这里集中调整配置或封装策略，避免业务代码直接散落依赖不同 JSON 实现。

### persistence

`pkg/persistence` 是数据实体和持久化操作层，核心入口是 `entity.Model`、`entity.ModelList[T]`、`types.SearchItem` 和 `types.IDataAction`。业务代码通常只操作模型列表，不直接处理数据库细节。

详见 [persistence/README.md](./persistence/README.md)。

### server

`pkg/server` 是服务提供层，负责服务生命周期、路由注册、REST/WebSocket 传输、内部服务调用、鉴权、安全配置、管理路由和运行时统计。

详见 [server/README.md](./server/README.md)。

### utils

`pkg/utils` 是通用工具函数集合，包含反射类型操作、对象映射、哈希、加密、IP/端口检测、文件操作、并发任务、事件发布、Snowflake ID 等能力。它被 `persistence`、`server`、`service/manage` 广泛使用。

其中反射相关函数如 `GetTypeName`、`GetPackageName`、`DeepFor`、`SetPropertyValue`、`NewArrayItem` 是路由自动推导、模型字段解析和管理界面自动生成的重要依赖。

## 暂不分析目录

- `dec`：当前暂不作为有效功能目录。
- `fileserver`：当前暂不作为有效功能目录。
- `localization`：当前暂不作为有效功能目录。

## 阅读顺序建议

1. 先读 `pkg/server/types/router.go`，理解业务路由接口。
2. 再读 `pkg/server/router/servicerouter.go`，理解包路径如何生成 API。
3. 读 `pkg/persistence/entity/model.go` 和 `pkg/persistence/entity/modellist.go`，理解业务模型和数据操作。
4. 读 `service/manage`，理解如何用极少代码生成后台 CRUD。
5. 最后读 `web/README.md`，理解前端如何消费后台自动生成的 view/search/command 接口。
