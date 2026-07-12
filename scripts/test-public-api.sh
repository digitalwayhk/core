#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT/scripts/public-api-common.sh"

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/digitalway-core-public-api-test.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

run_apidiff -w "$tmp_dir/old.apidiff" github.com/digitalwayhk/core/scripts/testdata/public-api/old
run_apidiff -w "$tmp_dir/compatible.apidiff" github.com/digitalwayhk/core/scripts/testdata/public-api/compatible
run_apidiff -w "$tmp_dir/incompatible.apidiff" github.com/digitalwayhk/core/scripts/testdata/public-api/incompatible

compare_public_api "$tmp_dir/old.apidiff" "$tmp_dir/compatible.apidiff"
if compare_public_api "$tmp_dir/old.apidiff" "$tmp_dir/incompatible.apidiff"; then
  echo "apidiff 未拒绝破坏性 API 变化" >&2
  exit 1
fi

echo "公共 API 工具契约测试通过"
