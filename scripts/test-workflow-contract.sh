#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workflow="$ROOT/.github/workflows/ci.yml"

fail() {
  echo "GitHub Actions 契约测试失败: $*" >&2
  exit 1
}

[[ -s "$workflow" ]] || fail "缺少 .github/workflows/ci.yml"

grep -Eq '^permissions:[[:space:]]*$' "$workflow" || fail "缺少 workflow 最小权限"
grep -Eq '^  contents:[[:space:]]+read[[:space:]]*$' "$workflow" || fail "contents 权限不是 read"
if grep -Eq '(^|[[:space:]])(write-all|contents:[[:space:]]*write|packages:[[:space:]]*write|pull-requests:[[:space:]]*write|id-token:[[:space:]]*write)' "$workflow"; then
  fail "workflow 含写权限"
fi

uses_lines="$(grep -E '^[[:space:]]*- uses:' "$workflow" || true)"
[[ -n "$uses_lines" ]] || fail "workflow 未使用任何 Action"
if grep -Ev 'uses:[[:space:]]+[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+@[0-9a-f]{40}([[:space:]]+#.*)?$' <<<"$uses_lines" >/dev/null; then
  fail "存在未锁定完整 SHA 的 Action"
fi

for gate in required/quick required/contracts required/server-manage required/race; do
  grep -Fq "./scripts/ci.sh $gate" "$workflow" || fail "缺少 gate: $gate"
done

[[ "$(grep -c 'timeout-minutes:' "$workflow")" -ge 4 ]] || fail "required job 缺少 timeout"
[[ "$(grep -c 'if:.*always()' "$workflow")" -ge 4 ]] || fail "required job 缺少 always artifact"
[[ "$(grep -c 'actions/upload-artifact@' "$workflow")" -ge 4 ]] || fail "required job 缺少 artifact 上传"
grep -Fq 'tools/go.sum' "$workflow" || fail "Go cache 未覆盖 tools/go.sum"
grep -Fq 'go.sum' "$workflow" || fail "Go cache 未覆盖根 go.sum"
grep -Eq 'cancel-in-progress:[[:space:]]+true' "$workflow" || fail "未取消同 ref 旧运行"
grep -Eq 'group:.*github\.workflow.*github\.(head_ref|ref)' "$workflow" || fail "concurrency 未按 workflow/ref 隔离"

if grep -Eq 'continue-on-error:|git[[:space:]]+(tag|push)|update-public-api|update-golden|go[[:space:]]+mod[[:space:]]+tidy' "$workflow"; then
  fail "required workflow 含吞错或修改型命令"
fi

echo "GitHub Actions 静态契约测试通过"
