#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT/scripts/public-api-common.sh"

verify_public_api_tool_version
manifest="$ROOT/api/public-api.txt"
if [[ ! -s "$manifest" ]]; then
  echo "公共 API manifest 不存在或为空；请显式运行 scripts/update-public-api.sh" >&2
  exit 1
fi
grep -Fxq "tool=$PUBLIC_API_TOOL_PACKAGE@$PUBLIC_API_TOOL_VERSION" "$manifest" || {
  echo "公共 API manifest 的工具版本与 tools/go.mod 不一致" >&2
  exit 1
}

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/digitalway-core-public-api-check.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT
expected_manifest="$tmp_dir/public-api.txt"
printf 'tool=%s@%s\n' "$PUBLIC_API_TOOL_PACKAGE" "$PUBLIC_API_TOOL_VERSION" >"$expected_manifest"

while IFS= read -r package; do
  baseline="$(public_api_baseline_name "$package")"
  baseline_path="$PUBLIC_API_BASELINE_DIR/$baseline"
  if [[ ! -s "$baseline_path" ]]; then
    echo "公共 API 基线缺失: $baseline_path" >&2
    exit 1
  fi
  printf 'package=%s baseline=%s\n' "$package" "$baseline" >>"$expected_manifest"
  run_apidiff -w "$tmp_dir/$baseline" "$package"
  compare_public_api "$baseline_path" "$tmp_dir/$baseline"
done < <(read_public_api_packages)

cmp -s "$manifest" "$expected_manifest" || {
  echo "公共 API manifest 与包清单不一致；请审查后显式更新基线" >&2
  diff -u "$manifest" "$expected_manifest" || true
  exit 1
}

expected_count="$(($(wc -l <"$expected_manifest") - 1))"
actual_count="$(find "$PUBLIC_API_BASELINE_DIR" -type f -name '*.apidiff' | wc -l | tr -d ' ')"
if [[ "$actual_count" != "$expected_count" ]]; then
  echo "公共 API 基线包含陈旧或多余文件: expected=$expected_count actual=$actual_count" >&2
  exit 1
fi

echo "公共 API 兼容性检查通过"
