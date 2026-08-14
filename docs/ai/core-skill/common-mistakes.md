# 高频错误快速自检


- URL 加入 `/public`、`/private`。
- private API 使用客户端提交的 UserID。
- `NewModel()` 未初始化嵌入指针。
- 无稳定 Code 的模型使用 BaseModel。
- `GetHash` 与业务唯一性、保存时间精度不一致。
- ManageService 传入内嵌实例而非真实 owner。
- 为切换 Manage 数据库而在 `OnSearchBefore` 手写查询并返回 `stop=true`，导致标准筛选、排序、分页和关联查询失效。
- public/private 直接返回持久化模型，或复用 Manage 列表 DTO。
- WebSocket 把外部用户订阅与内部 EventBridge 混为一谈。
- private WebSocket 未实现可信身份注入和用户级通知过滤。
- 绕过 models 持久化边界/`ServiceContext`，或在 API 层直接绑定具体数据库驱动。
- public/private 直接 `NewModelList` 或套用 Manage Search/CRUD 做业务读写（正确：models 业务方法 + `IDataAction`）。
- Manage 不用 `ModelList` 却手写 Search `stop=true` 破坏筛选分页；或该重写服务级 `GetList` 时未重写；分库场景用 per-market 自研列表代替 `IDBName` 标准管道。
- 动态分库时 MySQL `Database` 非空、缺 `marketCode` 仍默认真库、View 只带 ID 却期望命中分库。
- 动态分库只让 `GetRemoteDBName()` 返回空串当作 fail-closed。实现会回退 `GetLocalDBName()`（`entity.Model` 默认 `"models"`），结果静默扫默认库；必须用哨兵库名或两个方法一起置空，并在 `OnSearchBefore` 前置拦截。
- 手写 `CREATE TABLE`/`CREATE DATABASE`、`init.sql`、`migrations/` 目录、引入版本化迁移框架，或在业务代码调用 GORM `AutoMigrate`——建库建表与补列由框架在首次数据访问时自动完成。
- 把 `models/schema`（`EnsureStorage`）当成业务 DDL 层，为没有跨模型事务的新服务无条件生成该包。
- 在每个具体 model/API 里散落库连接，未在基础 model/store 集中 DataAction；或把「库类型」与「ModelList vs IDataAction」混为一谈。
- 集成测试重新实现公共 Suite，只测 handler，或依赖开发机已有配置和数据库。
- 仅因配置字段存在就声明能力稳定。
- 单元测试隐式依赖 Docker/本机数据库。
- 已需要高吞吐写时仍去找已删除的全局 `StartOrderWriteStore`、或使用兼容层 `SetSyncDB` 与 Manage 式列表轮询，未采用「本地可靠写 → `UseWriteBehind` → 远程权威库」。
- 水平扩展把最终业务库按副本分片，或用每进程私有库冒充共享 remote（开发用 SQLite、生产换共享 MySQL 是 DataAction 切换，不是分片）。
- 恢复 `RouterStats`/`Statistics`；Runtime 把未采集指标写成 0；浏览器直连 Prometheus 或其他实例 `/metrics`。
- 依赖 core 的业务仓库未安装 `.codex/skills/use-digitalway-core`，凭记忆编码（见 `docs/codex/CONSUMER_AI_SKILL_SETUP.md`）。
