# 04 - Manage Hooks

本示例展示如何用极少代码扩展标准管理功能：

- `DoBefore`：在 Add/Edit/Remove/Submit/Release 前做业务校验。
- `DoAfter`：在操作完成后补充审计、通知或返回值。
- `SearchBefore` / `SearchAfter`：改写查询条件或整理列表返回。

## 运行

```bash
go run ./examples/04-manage-hooks/main -p 18084
```

示例中 `ArticleManage` 会拒绝空标题，并在查询后返回标准 `TableData`。
