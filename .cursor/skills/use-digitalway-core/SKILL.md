---
name: use-digitalway-core
description: Use when 使用或审查 github.com/digitalwayhk/core 的服务、IRouter、基础资料 Model/业务事实 Model 分类、Model/Manage 继承、Manage 动态分库 IDBName、认证、WebSocket、缓存、本地可靠写、EventBridge、业务统计、经营分析、服务报表、多服务运行图 Runtime API、配置、集成测试、性能或兼容性时。
---

# 使用 Digitalway Core

**本文件是指针，不含规范正文。** 先按以下顺序找到权威源目录并读取其中的 `SKILL.md`：

1. `../core-skill/`（相对本文件的同级目录）—— 经 `scripts/link-consumer-skill.sh` 安装后的位置，项目级与用户级安装都适用。
2. `docs/ai/core-skill/`（仓库根起算）—— 在 core 仓库（`github.com/digitalwayhk/core`）本体中的位置。

读到后再按其「主题分片索引」按需读取同目录下的分片（`models.md`、`manage.md`、`routing-and-dto.md` 等）。不要凭本文件或记忆中的旧约定编码。

若两个路径都不存在，说明 skill 尚未安装：先在仓库根执行
`CORE=$(go list -m -f '{{.Dir}}' github.com/digitalwayhk/core) && "$CORE/scripts/link-consumer-skill.sh" --target .`
再继续开发。

所有 AI 代理（Codex、Claude、Cursor、GitHub Copilot）共用同一份权威源。修改规范只改权威源目录，不要把正文复制回本文件——历史上维护多份全文导致了规范双向漂移。
