# 03 - Manage CRUD

本示例展示 `service/manage.ManageService[T]`：

- 一个模型即可获得 View、Search、Add、Edit、Remove、Submit、Release。
- 模型嵌入 `*entity.BaseModel` 后，Submit 会把 `State` 从 `0` 改为 `1`。
- 该模式是管理后台最常用的低代码入口。

## 运行

```bash
go run ./examples/03-manage-crud/main -p 18083
```

## 常用接口

服务名为 `catalog`，管理路由路径由框架自动推导，通常形如：

- `/api/catalog/productmanage/view`
- `/api/catalog/productmanage/search`
- `/api/catalog/productmanage/add`
- `/api/catalog/productmanage/edit`
- `/api/catalog/productmanage/remove`
- `/api/catalog/productmanage/submit`
- `/api/catalog/productmanage/release`

具体路径可结合启动日志或 OpenAPI 页面确认。
