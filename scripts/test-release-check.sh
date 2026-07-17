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

prepare_release_fixture() {
	local fixture="$1"
	(cd "$fixture" &&
		git init -q &&
		git config user.name "release-check-test" &&
		git config user.email "release-check@example.invalid" &&
		git add . &&
		git commit -qm "fixture")
}

expect_release_failure() {
	local fixture="$1"
	local version="$2"
	local label="$3"
	prepare_release_fixture "$fixture"
	if (cd "$fixture" && CORE_RELEASE_VERSION="$version" ./scripts/release-check.sh --release >/dev/null 2>&1); then
		fail "$label 应被正式发布门禁拒绝"
	fi
}

valid="$(create_fixture valid)"
(cd "$valid" && ./scripts/release-check.sh --candidate >/dev/null) || fail "有效 fixture 应通过"

valid_chinese="$(create_fixture valid-chinese)"
sed 's/| core | futures | `migration_test.go` |/| 发布工具 | 多服务进程、跨节点通知扩展 | `迁移测试.go` |/' \
  "$valid_chinese/docs/codex/DEPRECATION_REGISTER.md" >"$valid_chinese/register.tmp"
mv "$valid_chinese/register.tmp" "$valid_chinese/docs/codex/DEPRECATION_REGISTER.md"
(cd "$valid_chinese" && ./scripts/release-check.sh --candidate >/dev/null) || fail "合法中文废弃登记应通过"

real_register="$(create_fixture real-register)"
cp "$ROOT/docs/codex/DEPRECATION_REGISTER.md" "$real_register/docs/codex/DEPRECATION_REGISTER.md"
(cd "$real_register" && ./scripts/release-check.sh --candidate >/dev/null) || fail "当前仓库废弃登记 smoke 应通过"

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

for placeholder_value in '-' 'N/A' 'TODO' '暂无' '—'; do
  fixture_name="placeholder-$(printf '%s' "$placeholder_value" | cksum | awk '{print $1}')"
  placeholder="$(create_fixture "$fixture_name")"
  sed "s#| \`migration_test.go\` |#| $placeholder_value |#" \
    "$placeholder/docs/codex/DEPRECATION_REGISTER.md" >"$placeholder/register.tmp"
  mv "$placeholder/register.tmp" "$placeholder/docs/codex/DEPRECATION_REGISTER.md"
  expect_failure "$placeholder" "废弃登记迁移证据占位符 $placeholder_value"
done

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

direct_minor="$(create_fixture direct-minor)"
cat >"$direct_minor/docs/codex/BREAKING_CHANGE_APPROVAL.md" <<'EOF'
# 破坏性变化批准

- 变更 ID：`socket-to-grpc-v1`
EOF
expect_release_failure "$direct_minor" "v0.1.0" "Socket 直接删除使用 MINOR"

blocked_consumer="$(create_fixture blocked-consumer)"
cat >"$blocked_consumer/docs/codex/BREAKING_CHANGE_APPROVAL.md" <<'EOF'
# 破坏性变化批准

- 变更 ID：`socket-to-grpc-v1`
EOF
printf '\nblocked-by-consumer-verification\n' >>"$blocked_consumer/docs/codex/CONSUMER_COMPATIBILITY_MATRIX.md"
(cd "$blocked_consumer" && ./scripts/release-check.sh --candidate >/dev/null) || fail "开发期 candidate 允许保留显式消费方阻断"
expect_release_failure "$blocked_consumer" "v1.0.0" "消费方证据未写回"

direct_major="$(create_fixture direct-major)"
cat >"$direct_major/docs/codex/BREAKING_CHANGE_APPROVAL.md" <<'EOF'
# 破坏性变化批准

- 变更 ID：`socket-to-grpc-v1`
EOF
prepare_release_fixture "$direct_major"
(cd "$direct_major" && CORE_RELEASE_VERSION=v1.0.0 ./scripts/release-check.sh --release >/dev/null) || fail "无消费方阻断的 MAJOR 应通过正式发布门禁"

echo "发布检查 shell 契约测试通过"
