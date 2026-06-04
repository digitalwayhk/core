# 项目说明：digitalwayhk/core 框架

本仓库是 `github.com/digitalwayhk/core` —— 一个商业级 Go 服务框架。

## 使用本框架开发后端 API 时

**请先阅读** `.github/copilot/skills/core-backend-api.md`，该文件涵盖：

- 服务注册与启动方式
- 数据库模型（`ModelList`、`BaseModel`）的定义规范
- API handler（Public / Private / Manage）的标准写法
- 前端调用 API 的 URL 规则与认证方式（TestToken 获取）
- 各类约定汇总

**关键原则（agent 必须遵守）：**

1. **不需要手动建库建表** — 调用 `NewModelList[T](nil)` 时框架会自动 `AutoMigrate`
2. **每个 model 文件对应一张表** — 所有对该表的操作都写在同一个文件里
3. **handler 通过 `req.GetUser()` 获取当前用户**（userid, username）
4. **Private API 需要 Bearer token** — 开发时用 `GET /api/{svc}/public/testtoken?userid=xxx&type=0` 获取
5. **Manage API 需要 type=1 的 token**（管理员权限）

## 关于 WayPage / JWT / Umi 路由

这些前端集成功能目前标记为 ⚠️ 暂不使用，不够稳定，agent 不应生成依赖这些的代码。

## 目录结构参考

```
cmd/{serviceName}/          # 服务入口
internal/core/{domain}/     # 业务逻辑（service / model / api）
```

参考已有实现：`internal/core/futures/`、`internal/core/trades/`
