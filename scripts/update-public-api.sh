#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source "$ROOT/scripts/public-api-common.sh"

verify_public_api_tool_version
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/digitalway-core-public-api.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

manifest="$tmp_dir/public-api.txt"
printf 'tool=%s@%s\n' "$PUBLIC_API_TOOL_PACKAGE" "$PUBLIC_API_TOOL_VERSION" >"$manifest"

while IFS= read -r package; do
  baseline="$(public_api_baseline_name "$package")"
  run_apidiff -w "$tmp_dir/$baseline" "$package"
  printf 'package=%s baseline=%s\n' "$package" "$baseline" >>"$manifest"
done < <(read_public_api_packages)

mkdir -p "$PUBLIC_API_BASELINE_DIR"
find "$PUBLIC_API_BASELINE_DIR" -type f -name '*.apidiff' -delete
mv "$tmp_dir"/*.apidiff "$PUBLIC_API_BASELINE_DIR"/
mv "$manifest" "$ROOT/api/public-api.txt"
echo "公共 API 基线已显式更新；提交前必须审查 api/public-api.txt 与 api/public-api/*.apidiff"
