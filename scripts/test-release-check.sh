#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_root="$(mktemp -d "${TMPDIR:-/tmp}/digitalway-release-check.XXXXXX")"
trap 'rm -rf "$tmp_root"' EXIT

fail() {
  echo "发布检查契约测试失败: $*" >&2
  exit 1
}

create_fixture() {
  local name="$1"
  local fixture="$tmp_root/$name"
  mkdir -p "$fixture/scripts" "$fixture/docs/codex"
  cp "$ROOT/scripts/release-check.sh" "$fixture/scripts/release-check.sh"
  cat >"$fixture/scripts/test.sh" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
  chmod +x "$fixture/scripts/test.sh" "$fixture/scripts/release-check.sh"
  cat >"$fixture/docs/RELEASE_POLICY.md" <<'EOF'
# 发布策略
EOF
  cat >"$fixture/docs/codex/CONSUMER_COMPATIBILITY_MATRIX.md" <<'EOF'
# 消费方矩阵
EOF
  cat >"$fixture/docs/codex/DEPRECATION_REGISTER.md" <<'EOF'
# 废弃 API 登记
| API | 替代入口 | 首次登记版本 | 最早删除版本 | Owner | 消费方 | 迁移证据 |
| --- | --- | --- | --- | --- | --- | --- |
| `OldAPI` | `NewAPI` | v0.0.248 | v0.1.0 | core | futures | `migration_test.go` |
EOF
  cat >"$fixture/CHANGELOG.md" <<'EOF'
# 变更日志
## [Unreleased]
### Added
- 新能力。
### Changed
- 兼容调整。
### Deprecated
- 旧入口。
### Removed
- 暂无。
### Fixed
- 修复。
### Security
- 安全加固。
## [0.0.247]
### Added
### Changed
### Deprecated
### Removed
### Fixed
### Security
EOF
  printf '%s\n' "$fixture"
}

expect_failure() {
  local fixture="$1"
  local label="$2"
  if (cd "$fixture" && ./scripts/release-check.sh --candidate >/dev/null 2>&1); then
    fail "$label 应被拒绝"
  fi
}

valid="$(create_fixture valid)"
(cd "$valid" && ./scripts/release-check.sh --candidate >/dev/null) || fail "有效 fixture 应通过"

wrong_section="$(create_fixture wrong-section)"
awk '
  /^## \[Unreleased\]/ { print; skip=1; next }
  /^## \[0\.0\.247\]/ { skip=0 }
  !skip { print }
' "$wrong_section/CHANGELOG.md" >"$wrong_section/CHANGELOG.tmp"
mv "$wrong_section/CHANGELOG.tmp" "$wrong_section/CHANGELOG.md"
expect_failure "$wrong_section" "Unreleased 缺标题但已发布段具备标题"

missing_field="$(create_fixture missing-field)"
sed 's/| core | futures |/|  | futures |/' "$missing_field/docs/codex/DEPRECATION_REGISTER.md" >"$missing_field/register.tmp"
mv "$missing_field/register.tmp" "$missing_field/docs/codex/DEPRECATION_REGISTER.md"
expect_failure "$missing_field" "废弃登记缺 Owner"

placeholder="$(create_fixture placeholder)"
sed 's/| `migration_test.go` |/| - |/' "$placeholder/docs/codex/DEPRECATION_REGISTER.md" >"$placeholder/register.tmp"
mv "$placeholder/register.tmp" "$placeholder/docs/codex/DEPRECATION_REGISTER.md"
expect_failure "$placeholder" "废弃登记迁移证据为占位符"

breaking="$(create_fixture breaking)"
sed 's/- 兼容调整。/- BREAKING: 删除旧接口。/' "$breaking/CHANGELOG.md" >"$breaking/changelog.tmp"
mv "$breaking/changelog.tmp" "$breaking/CHANGELOG.md"
expect_failure "$breaking" "破坏性变化缺迁移说明或批准文件"

breaking_migration="$(create_fixture breaking-migration)"
sed 's/- 兼容调整。/- BREAKING: 删除旧接口。\n- Migration: 使用 NewAPI。/' \
  "$breaking_migration/CHANGELOG.md" >"$breaking_migration/changelog.tmp"
mv "$breaking_migration/changelog.tmp" "$breaking_migration/CHANGELOG.md"
(cd "$breaking_migration" && ./scripts/release-check.sh --candidate >/dev/null) || fail "带迁移说明的破坏性变化应通过"

breaking_approved="$(create_fixture breaking-approved)"
sed 's/- 兼容调整。/- BREAKING: 删除旧接口。/' "$breaking_approved/CHANGELOG.md" >"$breaking_approved/changelog.tmp"
mv "$breaking_approved/changelog.tmp" "$breaking_approved/CHANGELOG.md"
cat >"$breaking_approved/docs/codex/BREAKING_CHANGE_APPROVAL.md" <<'EOF'
# 破坏性变化批准

Owner 已批准本次发布候选变更。
EOF
(cd "$breaking_approved" && ./scripts/release-check.sh --candidate >/dev/null) || fail "带批准文件的破坏性变化应通过"

echo "发布检查 shell 契约测试通过"
