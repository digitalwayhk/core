#!/usr/bin/env bash
# 校验 AI skill 的单一权威源约定，防止各 agent 目录重新出现全文副本。
#
# 背景：历史上 .codex 与 .github/copilot 各自维护一份全文 skill，导致规范双向漂移
# （"框架自动建库建表"只写在 Copilot 那份、TestToken URL 在 Copilot 那份里写错）。
# 现在正文只允许存在于 docs/ai/core-skill/，agent 目录下只放指针。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

AUTHORITATIVE="docs/ai/core-skill"
MQ_LIFECYCLE_GUIDE="docs/codex/MQ_MESSAGE_LIFECYCLE_GUIDE.md"
# 单文件上限。超过约 60 KB 会触发部分 agent 工具链的管道截断。
MAX_SHARD_BYTES=61440
# 指针文件上限。超过说明正文被复制回来了。
MAX_POINTER_BYTES=4096

POINTERS=(
  ".codex/skills/use-digitalway-core/SKILL.md"
  ".claude/skills/use-digitalway-core/SKILL.md"
  ".cursor/skills/use-digitalway-core/SKILL.md"
  ".github/copilot/skills/core-backend-api.md"
)

failures=0
fail() {
  echo "FAIL: $*" >&2
  failures=$((failures + 1))
}

filesize() { wc -c <"$1" | tr -d ' '; }

# 1. 权威源必须存在。
if [[ ! -f "$AUTHORITATIVE/SKILL.md" ]]; then
  echo "FAIL: 缺少权威源 $AUTHORITATIVE/SKILL.md" >&2
  exit 1
fi

# 2. 索引表引用的分片必须存在；反之分片也必须被索引，否则 agent 发现不了。
while IFS= read -r shard; do
  [[ -n "$shard" ]] || continue
  if [[ ! -f "$AUTHORITATIVE/$shard" ]]; then
    fail "$AUTHORITATIVE/SKILL.md 索引了不存在的分片: $shard"
  fi
done < <(grep -oE '\(([a-z0-9-]+\.md)\)' "$AUTHORITATIVE/SKILL.md" | tr -d '()' | sort -u)

for path in "$AUTHORITATIVE"/*.md; do
  name="$(basename "$path")"
  [[ "$name" == "SKILL.md" ]] && continue
  if ! grep -q "($name)" "$AUTHORITATIVE/SKILL.md"; then
    fail "分片 $name 未被 $AUTHORITATIVE/SKILL.md 索引，agent 无法发现它"
  fi
done

# 2.1 MQ 生命周期是跨 Provider 的安全契约，必须同时保留长期指南与 skill 入口。
if [[ ! -f "$MQ_LIFECYCLE_GUIDE" ]]; then
  fail "缺少 MQ 生命周期长期指南: $MQ_LIFECYCLE_GUIDE"
else
  for required in \
    "消息生命周期状态机" \
    "Provider 能力矩阵" \
    "安全回收前沿" \
    "新增 Provider" \
    "Bitzoom 接入示例"; do
    if ! grep -q "$required" "$MQ_LIFECYCLE_GUIDE"; then
      fail "$MQ_LIFECYCLE_GUIDE 缺少必需章节或关键词: $required"
    fi
  done
fi
if ! grep -q "MQ_MESSAGE_LIFECYCLE_GUIDE.md" "$AUTHORITATIVE/multiservice-and-observability.md"; then
  fail "multiservice-and-observability.md 未链接 MQ 生命周期长期指南"
fi
if ! grep -q "RequireMessageLifecycle" "$AUTHORITATIVE/multiservice-and-observability.md"; then
  fail "multiservice-and-observability.md 未声明 RequireMessageLifecycle 入口"
fi

# 3. 每个权威源文件都不能超过管道截断阈值。
for path in "$AUTHORITATIVE"/*.md; do
  size="$(filesize "$path")"
  if ((size > MAX_SHARD_BYTES)); then
    fail "$path 为 $size 字节，超过 $MAX_SHARD_BYTES；请继续拆分主题分片"
  fi
done

# 4. 各 agent 目录必须有指针文件，且必须小、且必须指向权威源。
for pointer in "${POINTERS[@]}"; do
  if [[ ! -f "$pointer" ]]; then
    fail "缺少 agent 指针文件: $pointer"
    continue
  fi
  size="$(filesize "$pointer")"
  if ((size > MAX_POINTER_BYTES)); then
    fail "$pointer 为 $size 字节，超过指针上限 $MAX_POINTER_BYTES；正文只能放在 $AUTHORITATIVE/"
  fi
  if ! grep -q "core-skill" "$pointer"; then
    fail "$pointer 未指向权威源 core-skill 目录"
  fi
done

# 5. agent skill 目录不得残留其他正文 md（历史 references/core-backend-api.md 已删除）。
is_pointer() {
  local candidate="${1#./}"
  local pointer
  for pointer in "${POINTERS[@]}"; do
    [[ "$candidate" == "$pointer" ]] && return 0
  done
  return 1
}

while IFS= read -r found; do
  [[ -n "$found" ]] || continue
  if ! is_pointer "$found"; then
    fail "agent skill 目录出现非指针正文文件: $found（正文只能放在 $AUTHORITATIVE/）"
  fi
done < <(find .codex/skills .claude/skills .cursor/skills .github/copilot/skills -name '*.md' -type f 2>/dev/null)

if ((failures > 0)); then
  echo "" >&2
  echo "AI skill 权威源校验失败：$failures 项。约定见 $AUTHORITATIVE/SKILL.md。" >&2
  exit 1
fi

shard_count="$(find "$AUTHORITATIVE" -name '*.md' -type f | wc -l | tr -d ' ')"
echo "AI skill 权威源校验通过：$AUTHORITATIVE 下 $shard_count 个文件，${#POINTERS[@]} 个 agent 指针。"
