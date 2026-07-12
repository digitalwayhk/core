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

unreleased_file="$(mktemp "${TMPDIR:-/tmp}/digitalway-unreleased.XXXXXX")"
trap 'rm -f "$unreleased_file"' EXIT
awk '
  /^## \[Unreleased\][[:space:]]*$/ { found=1; active=1; next }
  active && /^## \[/ { active=0; exit }
  active { print }
  END { if (!found) exit 2 }
' CHANGELOG.md >"$unreleased_file" || {
  echo "CHANGELOG 缺少 ## [Unreleased] 段" >&2
  exit 1
}

for heading in Added Changed Deprecated Removed Fixed Security; do
  count="$(grep -Ec "^### ${heading}[[:space:]]*$" "$unreleased_file" || true)"
  [[ "$count" == "1" ]] || { echo "CHANGELOG Unreleased 必须且只能包含一个 $heading 标题" >&2; exit 1; }
done

awk '
  function trim(value) {
    gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
    return value
  }
  function placeholder(value, lower) {
    lower=tolower(value)
    return value == "" || value == "-" || value == "—" || value == "暂无" ||
      lower == "n/a" || lower == "none" || lower ~ /todo|tbd|待确认/
  }
  /^\|[[:space:]]*API[[:space:]]*\|/ { header=1; next }
  header && /^\|[[:space:]-]+\|/ { next }
  header && /^\|/ {
    count=split($0, fields, "|")
    if (count < 9) {
      print "废弃登记列数不足: " $0 > "/dev/stderr"
      failed=1
      next
    }
    for (i=2; i<=8; i++) {
      fields[i]=trim(fields[i])
      if (placeholder(fields[i])) {
        print "废弃登记字段为空或为占位符（第 " (i-1) " 列）: " $0 > "/dev/stderr"
        failed=1
      }
    }
    if (fields[4] !~ /^v[0-9]+\.[0-9]+\.[0-9]+$/ || fields[5] !~ /^v[0-9]+\.[0-9]+\.[0-9]+$/) {
      print "废弃登记版本格式无效: " $0 > "/dev/stderr"
      failed=1
    }
    rows++
  }
  END {
    if (!header || rows == 0) {
      print "废弃登记缺少表头或数据行" > "/dev/stderr"
      failed=1
    }
    exit failed
  }
' docs/codex/DEPRECATION_REGISTER.md || exit 1

if grep -En '待确认|TODO|TBD' docs/codex/CONSUMER_COMPATIBILITY_MATRIX.md; then
  echo "发布契约仍含未确认占位" >&2
  exit 1
fi

if grep -Eiq 'BREAKING([ :]|$)|破坏性([变更：:]|$)' "$unreleased_file"; then
  if ! grep -Eiq 'Migration:|迁移说明[：:]' "$unreleased_file" &&
    ! test -s docs/codex/BREAKING_CHANGE_APPROVAL.md; then
    echo "Unreleased 含破坏性变化，但缺少迁移说明或批准文件" >&2
    exit 1
  fi
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
