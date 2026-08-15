# AGENTS.md

@/Users/vincent/.codex/RTK.md

## 项目说明

本仓库是 `github.com/digitalwayhk/core`，一个 Go 服务框架。

使用本框架开发或修改后端 API 前：

- 规范正文的**唯一权威源**是 `docs/ai/core-skill/`。先读 `docs/ai/core-skill/SKILL.md`，再按其「主题分片索引」按需读取分片。
- `.codex/skills/`、`.claude/skills/`、`.cursor/skills/`、`.github/copilot/skills/` 下只有指向权威源的指针文件，不含正文；不要在这些目录里保存副本。
- 当指南与当前代码、测试或公开契约不一致时，以当前代码、测试和公开契约为准，并同步修正 `docs/ai/core-skill/` 下对应分片。

**其他项目依赖本仓库时**：`go get` 不会自动安装 skill。消费方 AI 应按 [README「AI 助手与 Skill」](./README.md) 与 [消费方 AI Skill 安装与识别](./docs/codex/CONSUMER_AI_SKILL_SETUP.md) 先运行 `scripts/link-consumer-skill.sh`，再阅读消费方仓库内的 skill。

关键原则：

1. 建库、建表和补列由框架在**首次数据访问时**自动完成（`NewModelList[T](nil)` 只是构造列表，本身不建表）；不要另行建立迁移脚本或调用 GORM `AutoMigrate`。删列、改类型等破坏性变更不自动执行，见 `docs/ai/core-skill/models.md`。
2. handler 通过 `req.GetUser()` 获取当前用户信息。
3. Private、Manage、ServerManage 路由必须遵守各自的认证域，不得用路径猜测或跨域 token 替代。
4. 路由注册、目录结构和 TestToken 用法以现行 skill、示例与测试为准，不复制旧项目中的路径约定。

## 语言规范

- 与用户的所有对话、问题、进度更新、计划、总结和交付说明必须使用中文。
- 新增或修改项目文档时必须使用中文，除非用户明确要求使用其他语言。
- 代码标识符、API 名称、协议字段、命令、文件路径、第三方产品名称以及必须保持兼容的原文可以保留英文。
- 引用已有英文内容时，应优先使用中文解释其含义，避免无必要地整段复制英文。
