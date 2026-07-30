# 消费方 AI Skill 安装与识别

本文说明：**其他项目依赖 `github.com/digitalwayhk/core` 时，如何让 Codex / Copilot / Claude / Grok 等 AI 找到并使用 Core 的开发 skill**。

## 先记住的边界

| 能力 | 是否随 `go get` 自动获得 |
| --- | --- |
| Go 包、类型、API | 是 |
| `.codex/skills/use-digitalway-core` | **否** |
| `docs/codex/*` 与 `examples/*` 作为 Agent 工作区文件 | **否**（只在 module 目录或本地 clone 中） |

Agent 默认扫描的是**当前仓库**与用户级 skill 目录，不会把「模块缓存里的某次 core 版本」当成已安装 skill。  
因此需要在消费方**显式安装 skill 链接/副本**，并在 `AGENTS.md` 写明用法。README 中的说明是给人类与 AI 的**入口契约**，不是 Go toolchain 的自动钩子。

## 给消费方 AI 的固定流程（可直接执行）

当满足任一条件时，Agent **必须先完成 skill 安装再写业务代码**：

- 当前仓库 `go.mod` 依赖 `github.com/digitalwayhk/core`
- 用户要求「按 Digitalway Core / core 规范」开发或审查后端 API
- 提示词或 `AGENTS.md` 要求阅读 `use-digitalway-core`，但本地路径不存在

### 步骤

1. **检测**是否已有可读 skill：

```bash
test -f .codex/skills/use-digitalway-core/SKILL.md && echo ready || echo missing
```

2. **缺失则安装**（优先本机 core 源码，其次 module 目录）：

```bash
# 推荐：本机有 core 源码（与 replace 路径一致）
export DIGITALWAY_CORE_PATH=/path/to/digitalway.hk/core
"$DIGITALWAY_CORE_PATH/scripts/link-consumer-skill.sh" --target . --write-agents

# 仅有模块依赖时
CORE="$(go list -m -f '{{.Dir}}' github.com/digitalwayhk/core)"
"$CORE/scripts/link-consumer-skill.sh" --target . --write-agents
```

3. **阅读**并遵守：

- `.codex/skills/use-digitalway-core/SKILL.md`
- skill 内 `references/core-backend-api.md`
- Core 源码中的 `docs/codex/*` 与最近 `examples/*`（路径用 `go list -m -f '{{.Dir}}' github.com/digitalwayhk/core` 或 `DIGITALWAY_CORE_PATH`）

4. **冲突裁决**：指南与当前代码、测试、公开契约不一致时，以代码、测试和契约为准，并回写文档/skill（在 core 仓库改，不要只改消费方摘要）。

## 一键脚本

脚本路径：`scripts/link-consumer-skill.sh`。

| 选项 | 含义 |
| --- | --- |
| `--target DIR` | 消费方仓库根，默认当前目录 |
| `--mode symlink` | 默认；指向 core 源码，升级自动跟随 |
| `--mode copy` | 复制 skill 树；适合无软链环境（需随 core 升级重跑） |
| `--user-global` | 同时装到 `~/.codex/skills`（及已有父目录时的 `~/.claude/skills`、`~/.grok/skills`） |
| `--write-agents` | 向消费方 `AGENTS.md` 追加标准 Digitalway 段落（已存在同名标记则跳过） |
| `--dry-run` | 只打印操作 |

解析 Core 根目录的顺序：

1. 环境变量 `DIGITALWAY_CORE_PATH`
2. 本脚本所在 core 仓库根
3. 在 `--target` 下执行 `go list -m -f '{{.Dir}}' github.com/digitalwayhk/core`，且该目录含 skill

## 消费方 AGENTS.md 推荐片段

可复制以下内容到消费方仓库（或使用 `--write-agents` 自动追加）：

```markdown
## Digitalway Core（AI Skill）

本项目依赖 `github.com/digitalwayhk/core`。开发或修改其后端 API 前：

1. 确认存在 `.codex/skills/use-digitalway-core/SKILL.md`；缺失时在本仓库根执行:
   `DIGITALWAY_CORE_PATH=<core源码路径> <core>/scripts/link-consumer-skill.sh --target . --write-agents`
   或仅有模块依赖时:
   `CORE=$(go list -m -f '{{.Dir}}' github.com/digitalwayhk/core) && "$CORE/scripts/link-consumer-skill.sh" --target . --write-agents`
2. Codex / 兼容 Agent 必须阅读 `.codex/skills/use-digitalway-core/SKILL.md`，并按其 `references/` 与 core 仓库 `docs/codex`、`examples` 执行。
3. GitHub Copilot 阅读 `.github/copilot/skills/core-backend-api.md`（若已安装）。
4. 当指南与当前代码、测试或公开契约不一致时，以代码、测试和契约为准。
5. Core 文档与示例路径：优先 `go list -m -f '{{.Dir}}' github.com/digitalwayhk/core`，或 `go.mod` 的 `replace` / 环境变量 `DIGITALWAY_CORE_PATH`。

完整说明: core 仓库 `docs/codex/CONSUMER_AI_SKILL_SETUP.md`。
```

## 推荐工程形态

### 形态 A：本地 `replace` + 软链（最稳）

```go
// go.mod
replace github.com/digitalwayhk/core => ../core
```

```bash
DIGITALWAY_CORE_PATH=../core ../core/scripts/link-consumer-skill.sh --target . --write-agents
```

Agent 可同时改业务代码、对照 examples，skill 与文档同版本。

### 形态 B：仅版本依赖 + module 目录 skill

适合临时环境。module 升级后路径变化，需重跑脚本。若 module 压缩包中不含 `.codex`（少见但可能被工具裁剪），必须改用形态 A 或单独 clone。

### 形态 C：用户级 skill

```bash
DIGITALWAY_CORE_PATH=/path/to/core \
  /path/to/core/scripts/link-consumer-skill.sh --target . --user-global --write-agents
```

适合一人多仓；仍建议每个消费方仓库保留项目级 `.codex/skills` 链接，便于 CI 与同事复现。

## 无法「完全自动」的原因与可行替代

- **Go 模块系统**不会在 `go get` 后执行安装 skill 的钩子。
- **Git 子模块 / 依赖**也不会默认把 `.codex` 映射进消费方。
- 可行的「自动」是指：**AI 读到 README / 本文 / 消费方 AGENTS 后，按固定命令自行安装**，而不是静默魔法。

若团队需要更强约束，可在消费方：

- 增加 `make setup-ai` / `make bootstrap` 调用 `link-consumer-skill.sh`
- 在 CI 中 `test -f .codex/skills/use-digitalway-core/SKILL.md`
- 将 `DIGITALWAY_CORE_PATH` 写入开发环境文档或 direnv

## 与本仓库其他文档的关系

| 文档 | 用途 |
| --- | --- |
| [README.md](../../README.md) 「AI 助手与 Skill」 | 人类与 Agent 的入口 |
| `.codex/skills/use-digitalway-core/SKILL.md` | 开发/审查 Core 业务时的强制规范 |
| [FRAMEWORK_USAGE_GUIDE.md](./FRAMEWORK_USAGE_GUIDE.md) | 场景与示例选择 |
| [CONSUMER_COMPATIBILITY_MATRIX.md](./CONSUMER_COMPATIBILITY_MATRIX.md) | 生产消费方版本锁定与 smoke |
| [API_COMPATIBILITY_SURFACE.md](./API_COMPATIBILITY_SURFACE.md) | 公共 API 兼容面 |

## 故障排查

| 现象 | 处理 |
| --- | --- |
| Agent 报 skill not found | 在消费方跑 `link-consumer-skill.sh`；检查 `.codex/skills/use-digitalway-core/SKILL.md` |
| skill 能读但 docs/examples 打不开 | 设置 `DIGITALWAY_CORE_PATH` 或 `replace` 到完整 core 源码，不要只拷 `SKILL.md` |
| `go list` 找不到模块 | 先在消费方 `go mod download github.com/digitalwayhk/core` 或加入 `go.mod` require |
| 软链在 Windows/CI 失败 | `--mode copy`，并在升级 core 后重跑 |
| 升级 core 后规范过时 | symlink 模式通常自动跟随；copy 模式必须重跑脚本 |
