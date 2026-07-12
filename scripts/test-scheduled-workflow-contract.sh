#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workflow="$ROOT/.github/workflows/ci-scheduled.yml"

fail() {
  echo "定时 CI 契约测试失败: $*" >&2
  exit 1
}

[[ -s "$workflow" ]] || fail "缺少 ci-scheduled.yml"
grep -Eq '^[[:space:]]+schedule:' "$workflow" || fail "缺少 nightly schedule"
grep -Eq '^[[:space:]]+workflow_dispatch:' "$workflow" || fail "缺少 workflow_dispatch"
grep -Fq './scripts/ci.sh scheduled/stress' "$workflow" || fail "缺少 scheduled/stress"
grep -Fq './scripts/ci.sh scheduled/integration' "$workflow" || fail "缺少 scheduled/integration"
grep -Eq '^permissions:[[:space:]]*$' "$workflow" || fail "缺少最小权限"
grep -Eq '^  contents:[[:space:]]+read[[:space:]]*$' "$workflow" || fail "contents 权限不是 read"
if grep -Eq 'continue-on-error:|write-all|contents:[[:space:]]*write|git[[:space:]]+(tag|push)' "$workflow"; then
  fail "定时 workflow 含吞错、写权限或发布命令"
fi

uses_lines="$(grep -E '^[[:space:]]*- uses:' "$workflow" || true)"
if grep -Ev 'uses:[[:space:]]+[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+@[0-9a-f]{40}([[:space:]]+#.*)?$' <<<"$uses_lines" >/dev/null; then
  fail "存在未锁定完整 SHA 的 Action"
fi

[[ "$(grep -c 'timeout-minutes:' "$workflow")" -ge 2 ]] || fail "scheduled job 缺少 timeout"
[[ "$(grep -c 'if:.*always()' "$workflow")" -ge 4 ]] || fail "scheduled job 缺少 always summary/artifact"
[[ "$(grep -c 'GITHUB_STEP_SUMMARY' "$workflow")" -ge 2 ]] || fail "scheduled job 缺少真实状态 summary"
[[ "$(grep -c 'actions/upload-artifact@' "$workflow")" -ge 2 ]] || fail "scheduled job 缺少 artifact"
grep -Fq 'CORE_TEST_PERSISTENCE_PROJECT_NAME:' "$workflow" || fail "Docker job 缺少唯一 project name"

echo "定时 GitHub Actions 静态契约测试通过"
