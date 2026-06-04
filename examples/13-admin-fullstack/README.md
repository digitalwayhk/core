# 13 - Admin Fullstack

本示例说明后端 `ManageService` 与 `web/admin` WayPlus 前端的配合方式。

这是一个说明型示例，不重复复制整个 Ant Design Pro 项目。实际前端代码位于：

- `web/admin/routes.ts`
- `web/admin/src/pages/views/main.tsx`
- `web/admin/src/components/WayPlus/WayPage`
- `web/admin/src/components/WayPlus/WayTable`
- `web/admin/src/components/WayPlus/WayForm`

## 后端契约

后端管理服务通过 `ManageService[T]` 提供：

- View：返回页面 schema。
- Search：返回列表数据。
- Add/Edit/Remove：基础增删改。
- Submit/Release：提交与发布类状态操作。

前端 WayPlus 的核心流程：

1. `WayPage` 根据路由参数请求 View schema。
2. `WayTable` 根据 schema 渲染列和操作按钮。
3. `WayForm` 根据字段 schema 渲染表单。
4. 操作按钮调用后端 Add/Edit/Remove/Submit/Release。

## 建议运行方式

后端：

```bash
go run ./examples/03-manage-crud/main -p 18083
```

前端：

```bash
cd web/admin
npm install
npm run start:dev
```

## WayPlus 路由

在 `web/admin/routes.ts` 中注册 main 路由，指向 `src/pages/views/main.tsx`。main 页面读取 service/controller 信息后，把请求方法传给 WayPage。

## schema contract

本目录的 `schema_contract.go` 只用 Go 结构说明前后端交换的数据含义，方便阅读；真实类型以 `service/manage/view` 包为准。
