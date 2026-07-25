#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_root="$(mktemp -d "${TMPDIR:-/tmp}/digitalway-consumer-contract.XXXXXX")"
trap 'rm -rf "$tmp_root"' EXIT
repo="$tmp_root/futures-source"
mkdir -p "$repo/sample"
(
  cd "$repo"
  git init -q
  git config user.name contract
  git config user.email contract@example.invalid
  cat >go.mod <<'EOF'
module consumer-contract

go 1.26.5
EOF
  cat >sample/sample.go <<'EOF'
package sample

func Stable() bool { return true }
EOF
  cat >sample/sample_test.go <<'EOF'
package sample

import "testing"

func TestStable(t *testing.T) {
	if !Stable() { t.Fatal("not stable") }
}
EOF
  git add go.mod sample
  git commit -q -m fixture
)
commit="$(git -C "$repo" rev-parse HEAD)"
before="$(git -C "$repo" status --porcelain=v1 --untracked-files=all)"
CORE_FUTURES_REPO="$repo" CORE_FUTURES_COMMIT="$commit" CORE_FUTURES_TEST_PACKAGES=./sample/... \
  CORE_FUTURES_COMPILE_PACKAGES=./sample \
  "$ROOT/scripts/test-consumer-futures.sh" >"$tmp_root/passed.log"
grep -q 'CONSUMER_SMOKE_STATUS=passed' "$tmp_root/passed.log"
[[ "$before" == "$(git -C "$repo" status --porcelain=v1 --untracked-files=all)" ]]

set +e
CORE_FUTURES_REPO="$repo" CORE_FUTURES_COMMIT=0000000000000000000000000000000000000000 \
  "$ROOT/scripts/test-consumer-futures.sh" >"$tmp_root/blocked.log" 2>&1
status=$?
set -e
[[ "$status" == "3" ]] || { echo "缺失 commit 应返回 3，实际为 $status" >&2; exit 1; }
grep -q 'CONSUMER_SMOKE_STATUS=blocked' "$tmp_root/blocked.log"

echo "futures 消费方 smoke 契约测试通过"
