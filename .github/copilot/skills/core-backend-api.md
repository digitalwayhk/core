# Skill: core-backend-api

**本文件是指针，不含规范正文。** 使用 `github.com/digitalwayhk/core` 开发或审查后端 API 前，先按以下顺序找到权威源目录（下称 `<权威源>`）并读取它的 `SKILL.md`：

1. `.codex/skills/core-skill/` —— 消费方仓库经 `scripts/link-consumer-skill.sh` 安装后的位置。
2. `docs/ai/core-skill/` —— 在 core 仓库本体中的位置。

再按其「主题分片索引」按需读取 `<权威源>` 下对应分片：

| 任务 | 分片 |
| --- | --- |
| 目录结构、业务分层、启动组合根 | `project-layout.md` |
| IRouter、RouterInfo、路径规则、Public/Private、DTO | `routing-and-dto.md` |
| 模型分类与继承、持久化边界、建库建表与字段迁移 | `models.md` |
| Manage CRUD、Hook 继承、`GetList` 数据源、动态分库 | `manage.md` |
| 缓存、本地可靠写、write-behind、水平扩展 | `write-path-and-performance.md` |
| 多服务调用、EventBridge、WebSocket、Runtime 观测、日志 | `multiservice-and-observability.md` |
| 业务统计、经营分析、服务报表 | `stats-and-reports.md` |
| Casdoor 双域、Web Admin bootstrap | `auth-casdoor-and-admin.md` |
| 命名规范与开发设计流程 | `naming-and-workflow.md` |
| OpenAPI 与前端调用约定 | `openapi-and-frontend.md` |
| 集成测试、UAT 与发布门禁 | `testing-and-release.md` |
| 高频错误快速自检 | `common-mistakes.md` |

所有 AI 代理（Codex、Claude、Cursor、GitHub Copilot）共用同一份权威源。修改规范只改权威源目录，不要把正文复制回本文件——历史上本文件与 Codex 那份各自维护全文，导致规范双向漂移：例如"框架自动建库建表"一度只写在本文件里，而 TestToken 的 URL 在本文件里被错写成 `/api/{service}/public/testtoken`。
