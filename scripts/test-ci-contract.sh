#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CI_SCRIPT="$ROOT/scripts/ci.sh"
MATRIX="$ROOT/docs/codex/CI_QUALITY_GATE_MATRIX.md"
TEST_SCRIPT="${CI_CONTRACT_TEST_SCRIPT:-$ROOT/scripts/test.sh}"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/digitalway-core-ci-contract.XXXXXX")"
trap 'rm -rf "$tmp_dir"' EXIT

fail() {
  echo "CI 契约测试失败: $*" >&2
  exit 1
}

script_gates="$tmp_dir/script-gates"
matrix_gates="$tmp_dir/matrix-gates"
sed -n 's/^  \([a-z][a-z-]*\/[a-z][a-z-]*\))$/\1/p' "$CI_SCRIPT" | sort -u >"$script_gates"
sed -n 's/^| `\([a-z][a-z-]*\/[a-z][a-z-]*\)` |.*/\1/p' "$MATRIX" \
  | grep -E '^(required|observational|scheduled|consumer)/' | sort -u >"$matrix_gates"
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
grep -q 'CI_GATE_START gate=required/quick commit=' "$tmp_dir/failure.out" || fail "开始元数据缺 gate/commit"
grep -Eq ' os=[^[:space:]]+' "$tmp_dir/failure.out" || fail "开始元数据缺 OS"
grep -q ' command="' "$tmp_dir/failure.out" || fail "开始元数据缺命令"

required_commands="$(sed -n '/required\/quick)/,/;;/p; /required\/contracts)/,/;;/p; /required\/server-manage)/,/;;/p; /required\/race)/,/;;/p' "$CI_SCRIPT")"
if grep -Eq 'rtk|git (tag|push)|update-public-api|integration-|CORE_TEST_' <<<"$required_commands"; then
  fail "required gate 包含禁止命令或外部依赖"
fi

required_test_modes="$(sed -n '/^  quick)/,/^    ;;/p; /^  release-contract)/,/^    ;;/p; /^  concurrency-race)/,/^    ;;/p' "$TEST_SCRIPT")"
if grep -Eq 'rtk|git (tag|push)|update-public-api|integration-|CORE_TEST_' <<<"$required_test_modes"; then
  fail "required 调用的 test.sh 模式包含禁止命令或外部依赖"
fi

grep -q 'log=".*/required-quick.log"' "$tmp_dir/failure.out" || fail "END 元数据中的日志路径未加引号"

success_bin="$tmp_dir/success-bin"
mkdir -p "$success_bin"
cat >"$success_bin/go" <<'EOF'
#!/usr/bin/env bash
if [[ "${1:-}" == "version" ]]; then
  echo "go version go-test-contract test/arch"
fi
exit 0
EOF
chmod +x "$success_bin/go"
TMPDIR="$tmp_dir" PATH="$success_bin:$PATH" "$CI_SCRIPT" required/quick >"$tmp_dir/temp.out" 2>&1
temporary_log="$(LC_ALL=C sed -n 's/.* log="\([^"]*\)"$/\1/p' "$tmp_dir/temp.out")"
[[ -n "$temporary_log" ]] || fail "无法从 END 元数据解析临时日志路径"
[[ ! -e "$(dirname "$temporary_log")" ]] || fail "未设置 CI_ARTIFACT_DIR 时临时目录未清理"

tee_artifacts="$tmp_dir/tee-artifacts"
mkdir -p "$tee_artifacts"
ln -s /dev/full "$tee_artifacts/required-quick.log"
set +e
PATH="$success_bin:$PATH" CI_ARTIFACT_DIR="$tee_artifacts" \
  "$CI_SCRIPT" required/quick >"$tmp_dir/tee.out" 2>&1
tee_status=$?
set -e
[[ "$tee_status" != "0" ]] || fail "tee 写入失败时 gate 不应返回成功"

if [[ "${CI_CONTRACT_SKIP_SELF_TEST:-0}" != "1" ]]; then
  polluted_test_script="$tmp_dir/test-polluted.sh"
  awk '{ print; if ($0 == "  quick)") print "    rtk go test ./..." }' \
    "$ROOT/scripts/test.sh" >"$polluted_test_script"
  set +e
  CI_CONTRACT_TEST_SCRIPT="$polluted_test_script" CI_CONTRACT_SKIP_SELF_TEST=1 \
    "$ROOT/scripts/test-ci-contract.sh" >"$tmp_dir/polluted.out" 2>&1
  polluted_status=$?
  set -e
  [[ "$polluted_status" != "0" ]] || fail "required 下游模式污染未被检测"
fi

echo "CI shell 契约测试通过"
