# 02 - Model Persistence

本示例展示 Core 的实体和持久化入口：

- 模型嵌入 `*entity.Model`。
- 实现 `NewModel()` 供 `ModelList.NewItem()` 初始化基础字段。
- 使用 `ModelList[T]` 完成 Add、Save、SearchWhere 等操作。

默认数据库由框架配置决定。首次运行服务或调用持久化时，Core 会按当前配置创建本地默认存储。

## 运行

```bash
go run ./examples/02-model-persistence/main
```

该示例会创建一个 `Book` 并演示如何构造查询条件。不同环境的实际存储位置取决于当前配置文件。
