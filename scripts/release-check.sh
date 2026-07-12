#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
mode="${1:---candidate}"

case "$mode" in
  --candidate|--release) ;;
  *) echo "usage: scripts/release-check.sh [--candidate|--release]" >&2; exit 2 ;;
esac

required=(CHANGELOG.md docs/RELEASE_POLICY.md docs/codex/DEPRECATION_REGISTER.md docs/codex/CONSUMER_COMPATIBILITY_MATRIX.md)
for file in "${required[@]}"; do
  test -s "$file" || { echo "发布契约文件缺失: $file" >&2; exit 1; }
done

for heading in Added Changed Deprecated Removed Fixed Security; do
  grep -Fq "### $heading" CHANGELOG.md || { echo "CHANGELOG Unreleased 缺少 $heading" >&2; exit 1; }
done
if grep -En '待确认|TODO|TBD' docs/codex/DEPRECATION_REGISTER.md docs/codex/CONSUMER_COMPATIBILITY_MATRIX.md; then
  echo "发布契约仍含未确认占位" >&2
  exit 1
fi

if [[ "$mode" == "--release" ]]; then
  : "${CORE_RELEASE_VERSION:?--release 需要 CORE_RELEASE_VERSION=vX.Y.Z}"
  [[ "$CORE_RELEASE_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "版本格式无效: $CORE_RELEASE_VERSION" >&2; exit 1; }
  test -z "$(git status --porcelain)" || { echo "正式发布要求工作区干净" >&2; exit 1; }
  if git rev-parse -q --verify "refs/tags/$CORE_RELEASE_VERSION" >/dev/null; then
    echo "tag 已存在，禁止重写: $CORE_RELEASE_VERSION" >&2
    exit 1
  fi
fi

"$ROOT/scripts/test.sh" api-compat
"$ROOT/scripts/test.sh" public-api
"$ROOT/scripts/test.sh" config-contract
"$ROOT/scripts/test.sh" security
echo "发布契约检查通过（${mode}）；脚本未创建 tag、未 push、未发布"
