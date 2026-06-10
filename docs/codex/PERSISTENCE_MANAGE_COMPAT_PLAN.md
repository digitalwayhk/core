# Core Persistence / Manage 兼容计划

## 检查项

- 新模型必须嵌入 `*entity.Model` 或项目约定基础模型。
- 新模型必须实现 `NewModel()`。
- 有自然唯一键的模型必须实现 `GetHash()`。
- private 路由必须调用父级 `Parse/Validation` 并使用 `req.GetUser()`。
- manage CRUD 使用 `service/manage.ManageService[T]`，自定义 Operation 保持审计。

## 依赖项目重点

- `futures`：金额使用 `decimal.Decimal`，不得绕过服务 wrapper 的 model-list factory。
- `omni-flow-ai-grok`：外部接口、Agent 工具、工具日志、知识库模型的 hash 与查询条件需稳定。
