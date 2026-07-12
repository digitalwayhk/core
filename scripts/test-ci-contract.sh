#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CI_SCRIPT="$ROOT/scripts/ci.sh"
MATRIX="$ROOT/docs/codex/CI_QUALITY_GATE_MATRIX.md"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/digitalway-core-ci-contract.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

fail() {
  echo "CI 契约测试失败: $*" >&2
  exit 1
}

script_gates="$tmp_dir/script-gates"
matrix_gates="$tmp_dir/matrix-gates"
sed -n 's/^  \([a-z][a-z-]*\/[a-z][a-z-]*\))$/\1/p' "$CI_SCRIPT" | sort -u >"$script_gates"
sed -n 's/^| `\([a-z][a-z-]*\/[a-z][a-z-]*\)` |.*/\1/p' "$MATRIX" | sort -u >"$matrix_gates"
cmp -s "$script_gates" "$matrix_gates" || {
  diff -u "$matrix_gates" "$script_gates" >&2 || true
  fail "脚本与矩阵 gate 不闭合"
}

set +e
"$CI_SCRIPT" unknown/gate >"$tmp_dir/unknown.out" 2>&1
unknown_status=$?
set -e
[[ "$unknown_status" == "2" ]] || fail "未知 gate 应返回 2，实际为 $unknown_status"

fake_bin="$tmp_dir/fake-bin"
artifact_dir="$tmp_dir/artifacts with spaces"
mkdir -p "$fake_bin"
cat >"$fake_bin/go" <<'EOF'
#!/usr/bin/env bash
if [[ "${1:-}" == "version" ]]; then
  echo "go version go-test-contract test/arch"
  exit 0
fi
echo "模拟 go 失败" >&2
exit 17
EOF
chmod +x "$fake_bin/go"

set +e
PATH="$fake_bin:$PATH" CI_ARTIFACT_DIR="$artifact_dir" \
  "$CI_SCRIPT" required/quick >"$tmp_dir/failure.out" 2>&1
failure_status=$?
set -e
[[ "$failure_status" == "17" ]] || fail "子命令退出码未透传，实际为 $failure_status"
[[ -s "$artifact_dir/required-quick.log" ]] || fail "含空格的产物目录未生成日志"
grep -q '模拟 go 失败' "$artifact_dir/required-quick.log" || fail "失败日志内容缺失"
grep -q 'CI_GATE_END gate=required/quick exit_code=17' "$tmp_dir/failure.out" || fail "结束元数据不完整"

required_commands="$(sed -n '/required\/quick)/,/;;/p; /required\/contracts)/,/;;/p; /required\/server-manage)/,/;;/p; /required\/race)/,/;;/p' "$CI_SCRIPT")"
if grep -Eq 'rtk|git (tag|push)|update-public-api|integration-|CORE_TEST_' <<<"$required_commands"; then
  fail "required gate 包含禁止命令或外部依赖"
fi

echo "CI shell 契约测试通过"
