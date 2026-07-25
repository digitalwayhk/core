#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
workflow="$ROOT/.github/workflows/consumer-smoke.yml"

[[ -s "$workflow" ]] || { echo "缺少 consumer-smoke.yml" >&2; exit 1; }
grep -Eq '^[[:space:]]+workflow_dispatch:' "$workflow"
grep -Eq '^  contents:[[:space:]]+read[[:space:]]*$' "$workflow"
grep -Fq '203ff8eda53a9691d9409d3ee32aa5868fa1d61f' "$workflow"
grep -Fq './scripts/ci.sh consumer/futures' "$workflow"
grep -Fq 'CONSUMER_SMOKE_STATUS=blocked' "$workflow"
grep -Fq 'exit 3' "$workflow"
grep -Fq 'if: ${{ always() }}' "$workflow"
if grep -Eq 'continue-on-error:|git[[:space:]]+(tag|push)|persist-credentials:[[:space:]]*true' "$workflow"; then
  echo "消费方 workflow 含吞错、发布或凭据持久化" >&2
  exit 1
fi
uses_lines="$(grep -E '^[[:space:]]*- uses:' "$workflow")"
if grep -Ev 'uses:[[:space:]]+[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+@[0-9a-f]{40}([[:space:]]+#.*)?$' <<<"$uses_lines" >/dev/null; then
  echo "消费方 workflow 存在未锁定完整 SHA 的 Action" >&2
  exit 1
fi

echo "消费方 GitHub Actions 静态契约测试通过"
