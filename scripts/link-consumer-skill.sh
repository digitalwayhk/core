#!/usr/bin/env bash
# 将 Digitalway Core 的 AI skill 链接/同步到消费方仓库，供 Codex / Copilot / Claude / Grok 等识别。
#
# 用法（在消费方仓库根目录执行）:
#   DIGITALWAY_CORE_PATH=/path/to/core ./scripts/link-consumer-skill.sh
#   # 或当本脚本仍在 core 仓库内时:
#   /path/to/core/scripts/link-consumer-skill.sh
#   # 或仅有 go.mod 依赖时（从 module 目录解析）:
#   go list -m -f '{{.Dir}}' github.com/digitalwayhk/core | xargs -I{} {}/scripts/link-consumer-skill.sh
#
# 选项:
#   --mode symlink|copy   默认 symlink（推荐）；copy 适合无法使用软链的环境
#   --target DIR          消费方仓库根，默认当前目录
#   --user-global         同时安装到 ~/.codex/skills（及存在时的 ~/.claude/skills、~/.grok/skills）
#   --write-agents        若目标没有 AGENTS.md 中的 Digitalway skill 段落，则追加标准片段
#   --dry-run             只打印将要执行的操作
#   -h|--help             帮助
set -euo pipefail

MODE="symlink"
TARGET="$(pwd)"
USER_GLOBAL=0
WRITE_AGENTS=0
DRY_RUN=0

usage() {
  sed -n '2,/^[^#]/p' "$0" | sed '/^[^#]/d; s/^# \{0,1\}//'
  exit 0
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --mode)
      MODE="${2:-}"
      shift 2
      ;;
    --target)
      TARGET="${2:-}"
      shift 2
      ;;
    --user-global)
      USER_GLOBAL=1
      shift
      ;;
    --write-agents)
      WRITE_AGENTS=1
      shift
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    -h|--help)
      usage
      ;;
    *)
      echo "未知参数: $1" >&2
      exit 2
      ;;
  esac
done

if [[ "$MODE" != "symlink" && "$MODE" != "copy" ]]; then
  echo "--mode 必须是 symlink 或 copy" >&2
  exit 2
fi

TARGET="$(cd "$TARGET" && pwd)"

resolve_core_root() {
  if [[ -n "${DIGITALWAY_CORE_PATH:-}" ]]; then
    (cd "$DIGITALWAY_CORE_PATH" && pwd)
    return
  fi
  local script_root
  script_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
  if [[ -f "$script_root/.codex/skills/use-digitalway-core/SKILL.md" ]]; then
    echo "$script_root"
    return
  fi
  if command -v go >/dev/null 2>&1 && [[ -f "$TARGET/go.mod" ]]; then
    local mod_dir
    if mod_dir="$(cd "$TARGET" && go list -m -f '{{.Dir}}' github.com/digitalwayhk/core 2>/dev/null)" && \
       [[ -n "$mod_dir" && -f "$mod_dir/.codex/skills/use-digitalway-core/SKILL.md" ]]; then
      echo "$mod_dir"
      return
    fi
  fi
  return 1
}

if ! CORE_ROOT="$(resolve_core_root)"; then
  cat >&2 <<'EOF'
无法定位 Digitalway Core 源码中的 skill。

请任选其一后重试:
  1) export DIGITALWAY_CORE_PATH=/path/to/digitalway.hk/core
  2) 在消费方 go.mod 中依赖 github.com/digitalwayhk/core，并保证 module 目录含 .codex/skills
  3) 直接执行 core 仓库内的本脚本: /path/to/core/scripts/link-consumer-skill.sh --target /path/to/consumer

说明见 docs/codex/CONSUMER_AI_SKILL_SETUP.md
EOF
  exit 1
fi

SKILL_SRC="$CORE_ROOT/.codex/skills/use-digitalway-core"
COPILOT_SRC="$CORE_ROOT/.github/copilot/skills/core-backend-api.md"
if [[ ! -f "$SKILL_SRC/SKILL.md" ]]; then
  echo "core skill 不完整: 缺少 $SKILL_SRC/SKILL.md" >&2
  exit 1
fi

run() {
  if [[ "$DRY_RUN" -eq 1 ]]; then
    echo "DRY-RUN: $*"
    return 0
  fi
  "$@"
}

install_link_or_copy() {
  local src="$1"
  local dest="$2"
  local dest_dir
  dest_dir="$(dirname "$dest")"
  run mkdir -p "$dest_dir"
  if [[ -e "$dest" || -L "$dest" ]]; then
    if [[ -L "$dest" ]]; then
      run rm -f "$dest"
    elif [[ -d "$dest" && "$MODE" == "copy" ]]; then
      run rm -rf "$dest"
    elif [[ -f "$dest" && "$MODE" == "copy" ]]; then
      run rm -f "$dest"
    else
      echo "目标已存在且不是可安全覆盖的符号链接: $dest" >&2
      echo "请手动删除后重试，或使用 --mode copy" >&2
      exit 1
    fi
  fi
  if [[ "$MODE" == "symlink" ]]; then
    run ln -sfn "$src" "$dest"
  else
    if [[ -d "$src" ]]; then
      run mkdir -p "$dest"
      run cp -R "$src/." "$dest/"
    else
      run cp "$src" "$dest"
    fi
  fi
  if [[ "$DRY_RUN" -eq 1 ]]; then
    echo "DRY-RUN: 将安装 $dest -> $src ($MODE)"
  else
    echo "已安装: $dest -> $src ($MODE)"
  fi
}

SKILL_DEST="$TARGET/.codex/skills/use-digitalway-core"
COPILOT_DEST="$TARGET/.github/copilot/skills/core-backend-api.md"

install_link_or_copy "$SKILL_SRC" "$SKILL_DEST"
if [[ -f "$COPILOT_SRC" ]]; then
  install_link_or_copy "$COPILOT_SRC" "$COPILOT_DEST"
fi

if [[ "$USER_GLOBAL" -eq 1 ]]; then
  for home_skills in \
    "${HOME}/.codex/skills" \
    "${HOME}/.claude/skills" \
    "${HOME}/.grok/skills"
  do
    if [[ -d "$(dirname "$home_skills")" ]] || [[ "$home_skills" == "${HOME}/.codex/skills" ]]; then
      run mkdir -p "$home_skills"
      install_link_or_copy "$SKILL_SRC" "$home_skills/use-digitalway-core"
    fi
  done
fi

AGENTS_MARKER="use-digitalway-core"
AGENTS_SNIPPET=$(cat <<'EOF'

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
EOF
)

if [[ "$WRITE_AGENTS" -eq 1 ]]; then
  agents_file="$TARGET/AGENTS.md"
  if [[ ! -f "$agents_file" ]]; then
    if [[ "$DRY_RUN" -eq 1 ]]; then
      echo "DRY-RUN: 将创建 $agents_file"
    else
      {
        echo "# AGENTS.md"
        echo "$AGENTS_SNIPPET"
      } >"$agents_file"
      echo "已创建 $agents_file"
    fi
  elif grep -q "$AGENTS_MARKER" "$agents_file"; then
    echo "AGENTS.md 已包含 Digitalway skill 说明，跳过追加"
  else
    if [[ "$DRY_RUN" -eq 1 ]]; then
      echo "DRY-RUN: 将向 $agents_file 追加 Digitalway skill 段落"
    else
      printf '%s\n' "$AGENTS_SNIPPET" >>"$agents_file"
      echo "已向 $agents_file 追加 Digitalway skill 段落"
    fi
  fi
fi

cat <<EOF

完成。
  Core 根目录: $CORE_ROOT
  消费方根目录: $TARGET
  Skill: $SKILL_DEST

建议在消费方 AGENTS.md / Claude.md 中要求 Agent 在修改 core 相关 API 前先读该 skill。
详细说明: $CORE_ROOT/docs/codex/CONSUMER_AI_SKILL_SETUP.md
EOF
