#!/usr/bin/env bash
# 契约测试：scripts/check-web-dist-sync.sh
# 把被测脚本复制进临时仓库运行，绝不修改真实 pkg/server/run/dist 或真实子模块。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHECK_SCRIPT="${ROOT}/scripts/check-web-dist-sync.sh"
REAL_DIST="${ROOT}/pkg/server/run/dist"

CONSISTENT_COMMIT="43f218e99f26f5ad7d97e085f37d11fa041b22d4"
DRIFTED_COMMIT="3088e40e27186e45d9af51e278223abaa9b6c71c"

fail() {
  echo "check-web-dist-sync 契约测试失败: $*" >&2
  exit 1
}

content_tree_hash() {
  local dir="$1"
  (
    cd "${dir}"
    find . -type f -print |
      LC_ALL=C sort |
      while IFS= read -r file; do
        shasum -a 256 "${file}"
      done |
      shasum -a 256 |
      awk '{print $1}'
  )
}

[[ -f "${CHECK_SCRIPT}" ]] || fail "scripts/check-web-dist-sync.sh 不存在"

# 真实产物与真实子模块指针的基线，收尾时必须一致。
real_fingerprint=""
if [[ -d "${REAL_DIST}" ]]; then
  real_fingerprint="$(content_tree_hash "${REAL_DIST}")"
fi
real_pointer_before="$(git -C "${ROOT}" ls-tree HEAD -- web/admin 2>/dev/null || true)"
real_submodule_head_before="$(git -C "${ROOT}/web/admin" rev-parse HEAD 2>/dev/null || true)"

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/core-check-web-dist-sync.XXXXXX")"
trap 'rm -rf -- "${tmp_dir}"' EXIT

fixture="${tmp_dir}/repo"

# 造一个只有 scripts/ 与 dist/ 的最小仓库：被测脚本按 BASH_SOURCE 的上级目录解析
# ROOT，复制进去后它看到的就是 fixture，而不是真实仓库。
reset_fixture() {
  rm -rf "${fixture}"
  mkdir -p "${fixture}/scripts" "${fixture}/pkg/server/run/dist"
  cp "${CHECK_SCRIPT}" "${fixture}/scripts/check-web-dist-sync.sh"
  git -C "${fixture}" init -q
  git -C "${fixture}" config user.email "contract@test.local"
  git -C "${fixture}" config user.name "contract"
}

write_build_info() {
  local commit="$1"
  printf '{\n  "frontend_commit": "%s",\n  "artifact_sha256": "%s"\n}\n' \
    "${commit}" "0000000000000000000000000000000000000000000000000000000000000000" \
    >"${fixture}/pkg/server/run/dist/build-info.json"
}

# 不需要真实子模块：直接往索引写 gitlink 条目即可复现 ls-tree 的 160000 输出。
add_submodule_pointer() {
  local commit="$1"
  git -C "${fixture}" update-index --add --cacheinfo "160000,${commit},web/admin"
}

commit_fixture() {
  git -C "${fixture}" add -A scripts pkg
  git -C "${fixture}" commit -q -m "fixture"
}

run_check() {
  local out="$1"
  local status=0
  bash "${fixture}/scripts/check-web-dist-sync.sh" >"${out}" 2>&1 || status=$?
  return "${status}"
}

# --- 1) 指针与 frontend_commit 一致 → 通过 ---
reset_fixture
write_build_info "${CONSISTENT_COMMIT}"
add_submodule_pointer "${CONSISTENT_COMMIT}"
commit_fixture
run_check "${tmp_dir}/ok.out" || fail "一致时应通过: $(cat "${tmp_dir}/ok.out")"
grep -q "${CONSISTENT_COMMIT}" "${tmp_dir}/ok.out" || fail "通过输出应包含 commit 值"
grep -q '校验通过' "${tmp_dir}/ok.out" || fail "通过输出应说明校验通过"

# --- 2) 不一致 → 失败，且同时打印两个 commit 与可操作提示 ---
reset_fixture
write_build_info "${DRIFTED_COMMIT}"
add_submodule_pointer "${CONSISTENT_COMMIT}"
commit_fixture
set +e
run_check "${tmp_dir}/drift.out"
drift_status=$?
set -e
[[ "${drift_status}" -ne 0 ]] || fail "指针与 frontend_commit 不一致时应失败"
grep -q "${CONSISTENT_COMMIT}" "${tmp_dir}/drift.out" ||
  fail "漂移报错应包含子模块指针 ${CONSISTENT_COMMIT}: $(cat "${tmp_dir}/drift.out")"
grep -q "${DRIFTED_COMMIT}" "${tmp_dir}/drift.out" ||
  fail "漂移报错应包含 frontend_commit ${DRIFTED_COMMIT}: $(cat "${tmp_dir}/drift.out")"
grep -q 'build-web-admin.sh' "${tmp_dir}/drift.out" || fail "漂移报错应给出重建 dist 的可操作指引"

# --- 3) 已提交指针前进但 dist 未重建：必须读 HEAD 指针而不是子模块工作区 ---
reset_fixture
write_build_info "${DRIFTED_COMMIT}"
add_submodule_pointer "${CONSISTENT_COMMIT}"
commit_fixture
# 子模块工作区停在旧 commit（与 dist 一致）时也不得放行：事实源是 HEAD 里的指针。
mkdir -p "${fixture}/web/admin"
git -C "${fixture}/web/admin" init -q
git -C "${fixture}/web/admin" config user.email "contract@test.local"
git -C "${fixture}/web/admin" config user.name "contract"
printf 'stale\n' >"${fixture}/web/admin/marker.txt"
git -C "${fixture}/web/admin" add -A
git -C "${fixture}/web/admin" commit -q -m "stale worktree"
set +e
run_check "${tmp_dir}/worktree.out"
worktree_status=$?
set -e
[[ "${worktree_status}" -ne 0 ]] || fail "子模块工作区落后时仍应按 HEAD 指针判定失败"
grep -q "${CONSISTENT_COMMIT}" "${tmp_dir}/worktree.out" || fail "应报告 HEAD 中的已提交指针"

# --- 4) build-info.json 缺失 → 明确失败 ---
reset_fixture
add_submodule_pointer "${CONSISTENT_COMMIT}"
commit_fixture
rm -f "${fixture}/pkg/server/run/dist/build-info.json"
set +e
run_check "${tmp_dir}/missing.out"
missing_status=$?
set -e
[[ "${missing_status}" -ne 0 ]] || fail "缺 build-info.json 应失败"
grep -q '缺少' "${tmp_dir}/missing.out" || fail "缺 build-info.json 应说明缺少该文件: $(cat "${tmp_dir}/missing.out")"
grep -q 'build-info.json' "${tmp_dir}/missing.out" || fail "缺文件报错应点名 build-info.json"

# --- 5) build-info.json 非法 JSON → 明确失败，不得崩成堆栈 ---
reset_fixture
add_submodule_pointer "${CONSISTENT_COMMIT}"
printf '{ this is not json\n' >"${fixture}/pkg/server/run/dist/build-info.json"
commit_fixture
set +e
run_check "${tmp_dir}/badjson.out"
badjson_status=$?
set -e
[[ "${badjson_status}" -ne 0 ]] || fail "非法 JSON 应失败"
grep -q '不是合法 JSON' "${tmp_dir}/badjson.out" || fail "非法 JSON 应给出明确原因: $(cat "${tmp_dir}/badjson.out")"
if grep -q 'Traceback' "${tmp_dir}/badjson.out"; then
  fail "非法 JSON 不应暴露 python 堆栈"
fi

# --- 6) 缺 frontend_commit 字段 → 明确失败 ---
reset_fixture
add_submodule_pointer "${CONSISTENT_COMMIT}"
printf '{\n  "artifact_sha256": "abc"\n}\n' >"${fixture}/pkg/server/run/dist/build-info.json"
commit_fixture
set +e
run_check "${tmp_dir}/nofield.out"
nofield_status=$?
set -e
[[ "${nofield_status}" -ne 0 ]] || fail "缺 frontend_commit 字段应失败"
grep -q 'frontend_commit' "${tmp_dir}/nofield.out" || fail "缺字段报错应点名 frontend_commit"

# --- 7) frontend_commit 为空 → 明确失败，不得当成一致 ---
reset_fixture
add_submodule_pointer "${CONSISTENT_COMMIT}"
printf '{\n  "frontend_commit": "",\n  "artifact_sha256": "abc"\n}\n' \
  >"${fixture}/pkg/server/run/dist/build-info.json"
commit_fixture
set +e
run_check "${tmp_dir}/emptyfield.out"
emptyfield_status=$?
set -e
[[ "${emptyfield_status}" -ne 0 ]] || fail "空 frontend_commit 应失败"
grep -q 'frontend_commit' "${tmp_dir}/emptyfield.out" || fail "空字段报错应点名 frontend_commit"

# --- 8) HEAD 中没有 web/admin 条目（子模块未登记）→ 明确失败 ---
reset_fixture
write_build_info "${CONSISTENT_COMMIT}"
commit_fixture
set +e
run_check "${tmp_dir}/nopointer.out"
nopointer_status=$?
set -e
[[ "${nopointer_status}" -ne 0 ]] || fail "取不到子模块指针时应失败"
grep -q 'web/admin' "${tmp_dir}/nopointer.out" || fail "取不到指针的报错应点名 web/admin"
grep -Eq '子模块条目|ls-tree' "${tmp_dir}/nopointer.out" ||
  fail "取不到指针应说明 HEAD 中没有子模块条目: $(cat "${tmp_dir}/nopointer.out")"

# --- 9) web/admin 被提交成普通文件而非 gitlink → 明确失败 ---
reset_fixture
write_build_info "${CONSISTENT_COMMIT}"
mkdir -p "${fixture}/web"
printf 'not a submodule\n' >"${fixture}/web/admin"
git -C "${fixture}" add -A scripts pkg web
git -C "${fixture}" commit -q -m "web/admin as blob"
set +e
run_check "${tmp_dir}/notgitlink.out"
notgitlink_status=$?
set -e
[[ "${notgitlink_status}" -ne 0 ]] || fail "web/admin 非 gitlink 时应失败"
grep -q '子模块条目' "${tmp_dir}/notgitlink.out" ||
  fail "非 gitlink 应说明不是子模块条目: $(cat "${tmp_dir}/notgitlink.out")"

# --- 10) 仓库无任何提交（HEAD 解析不了）→ 明确失败，不得静默通过 ---
reset_fixture
write_build_info "${CONSISTENT_COMMIT}"
set +e
run_check "${tmp_dir}/nohead.out"
nohead_status=$?
set -e
[[ "${nohead_status}" -ne 0 ]] || fail "HEAD 解析不了时应失败"
grep -q 'HEAD' "${tmp_dir}/nohead.out" || fail "无 HEAD 应说明无法解析 HEAD: $(cat "${tmp_dir}/nohead.out")"

# --- 收尾：真实 dist 与真实子模块指针必须原样 ---
if [[ -n "${real_fingerprint}" ]]; then
  after_fingerprint="$(content_tree_hash "${REAL_DIST}")"
  [[ "${after_fingerprint}" == "${real_fingerprint}" ]] ||
    fail "契约测试改动了真实 pkg/server/run/dist 内容"
fi
real_pointer_after="$(git -C "${ROOT}" ls-tree HEAD -- web/admin 2>/dev/null || true)"
[[ "${real_pointer_after}" == "${real_pointer_before}" ]] || fail "契约测试改动了真实子模块指针"
real_submodule_head_after="$(git -C "${ROOT}/web/admin" rev-parse HEAD 2>/dev/null || true)"
[[ "${real_submodule_head_after}" == "${real_submodule_head_before}" ]] ||
  fail "契约测试改动了真实子模块工作区 HEAD"

echo "check-web-dist-sync 契约测试通过"
