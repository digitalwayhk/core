# Manage ModelList 模型边界设计

## 目标

统一 Core 对 Manage 持久化边界的定义：Manage 使用 `ModelList` 获得筛选、排序、分页和 CRUD 生命周期，但不得感知、选择或传递 `IDataAction`。数据库连接、local/remote 选择和动态分库策略全部由服务的 models 层决定。

本次只修改 Core 框架、权威 skill、标准示例和测试；不修改任何消费方项目。

## 对外模型入口

每个服务的 models 持久化组合根默认只向 Manage 暴露一个通用构造器：

```go
// NewManageModelList 为当前服务的 Manage API 创建模型列表。
//
// 本方法是 Manage 访问 ModelList 的唯一 models 层入口。调用方只声明要管理的
// 模型类型，不得感知或传入 IDataAction，也不得决定使用本地库、远程权威库或
// 动态分库适配器。连接类型、数据库位置和路由策略全部由当前服务的 models
// 持久化组合根集中选择，因此切换存储策略时不需要修改 api/manage。
func NewManageModelList[T persistencetypes.IModel]() *entity.ModelList[T] {
	return entity.NewModelList[T](manageDataAction())
}
```

`manageDataAction()`、`localDataAction()` 和 `remoteDataAction()` 是 models 内部实现细节，默认不导出。`NewManageModelList` 不接受可选 `IDataAction`，测试替身应注入 models/internal/store，而不是从 Manage 注入。

只有某类模型确实使用另一套独立权威库、集群或存储类型时，才增加业务语义明确的专用构造器，例如 `NewArchiveManageModelList[T]()`。普通按市场分库仍使用 `NewManageModelList[T]()`，由模型 `SearchWhere`、`GetRemoteDBName()` 和 models 内可路由适配器完成单次查询的选库。

## Manage 调用边界

服务级 Manage 基座统一实现：

```go
func (*ServiceManage[T]) GetList() interface{} {
	return models.NewManageModelList[T]()
}
```

`api/manage` 不得：

- 导入 `pkg/persistence/types` 以取得 `IDataAction`；
- 调用 `ManageDataAction`、`RemoteDataAction` 或 `LocalDataAction`；
- 直接调用 `entity.NewModelList` 选择数据源；
- 为测试给 Manage owner 注入 `IDataAction`；
- 在自定义命令中打开事务或直接执行 `Load/Insert/Update/Delete`。

普通 CRUD 通过 `GetList` 进入 models。附加查询调用 models 的业务语义方法；复杂命令调用 business，再由 business/models 管理事务和持久化。

## Core 生命周期修复

当前 `ManageService.SearchAfter` 在默认项回填时调用基类 `own.GetList()`。Go 嵌入不提供虚方法分派，因此该调用绕过最终 owner 的 `GetList()`，可能把默认项写入默认 SQLite。

修复后，默认项回填必须从最终 owner 解析 `IGetModelList` 并创建列表，与 View、Search 和 Operation 使用同一数据源选择链。无法取得有效 `ModelList[T]` 时返回明确错误，不静默回退到另一数据库。

该修改不删除 `ManageService.GetList()`，也不改变 `entity.NewModelList` 的公共签名，保持现有消费方源码兼容。

## 示例和权威文档

更新 `docs/ai/core-skill/` 的 models、manage、project-layout、common-mistakes 和入口契约：

- 删除“在 `GetList` 中传入 `models.ManageDataAction()`”的推荐；
- 使用 `NewManageModelList[T]()` 展示生产、共享权威库和动态分库；
- 明确 `IDataAction` 只存在于 models 持久化实现；
- 修正通用 Button 直接 `entity.NewModelList(nil)` 的示例；
- 指针 skill 不复制规范正文。

标准示例至少覆盖：

- 简单本地 SQLite 服务的 `NewManageModelList`；
- 多服务 models 持久化组合根；
- 动态分库通过同一个 `NewManageModelList` 完成路由，而不是暴露专用 DataAction。

## 测试策略

按测试先行实施：

1. 增加回归测试，证明默认项回填调用最终 owner 的 `GetList()`；测试先在旧实现上失败。
2. 增加失败数据源测试，证明默认项不会写入基类默认 SQLite。
3. 增加示例契约测试或静态架构测试，禁止 `examples/**/api/manage` 直接出现 `IDataAction`、`*DataAction()` 和 `entity.NewModelList(...)`。
4. 更新示例编译测试，证明 `NewManageModelList[T]()` 是 Manage 的唯一模型列表入口。
5. 运行 Manage 定向测试、示例测试、race、API/release contract 和日志门禁。

## 兼容与迁移

这是加性规范和内部行为修复：

- 保留 `entity.NewModelList[T](action)` 给 models、框架内部和兼容消费方使用；
- 保留 `ManageService.GetList()` 的默认本地 SQLite 行为；
- 不在 Core 公共包增加包含业务连接策略的新工厂接口；
- 消费方现有 `NewManageList` 或 `ManageDataAction` 先由各项目按文件迁移，Core 本次不直接删除其源码 API。

Core 完成后单独输出 Bitzoom 的逐文件迁移顺序，不在本次提交中修改 Bitzoom。
