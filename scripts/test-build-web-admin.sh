#!/usr/bin/env bash
# 契约测试：scripts/build-web-admin.sh
# 使用临时假 admin/dist/target，绝不修改真实 pkg/server/run/dist。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_SCRIPT="${ROOT}/scripts/build-web-admin.sh"
REAL_DIST="${ROOT}/pkg/server/run/dist"
REAL_BACKUP="${REAL_DIST}.backup"

fail() {
  echo "build-web-admin 契约测试失败: $*" >&2
  exit 1
}

content_tree_hash() {
  # 与 artifact 类似：相对路径排序后对每个文件做 sha256，再对清单做 sha256
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

if [[ ! -f "${BUILD_SCRIPT}" ]]; then
  fail "scripts/build-web-admin.sh 不存在"
fi

# --- 0a) 依赖安装必须尊重锁文件，并仅在安装阶段禁用 husky ---
if ! grep -E '^[[:space:]]*HUSKY=0[[:space:]]+npm[[:space:]].*[[:space:]]ci([[:space:]]|$)' "${BUILD_SCRIPT}" | grep -q .; then
  fail "scripts/build-web-admin.sh 的 npm ci 必须前缀 HUSKY=0，避免 husky prepare 写 git config"
fi
if ! grep -q 'yarn.lock' "${BUILD_SCRIPT}"; then
  fail "scripts/build-web-admin.sh 必须识别 yarn.lock"
fi
if ! grep -E '^[[:space:]]*HUSKY=0[[:space:]]+yarn[[:space:]].*install.*--frozen-lockfile' "${BUILD_SCRIPT}" | grep -q .; then
  fail "scripts/build-web-admin.sh 的 yarn install 必须锁定版本并禁用 husky"
fi
# 确保 jest / tsc:auth / build 行不是 HUSKY=0 前缀（只禁用 ci 的 prepare）
while IFS= read -r line; do
  if echo "${line}" | grep -Eq 'npm[[:space:]].*run[[:space:]]+(jest|tsc:auth|build)'; then
    if echo "${line}" | grep -Eq 'HUSKY=0'; then
      fail "jest/tsc:auth/build 不应前缀 HUSKY=0（仅 npm ci 需要）: ${line}"
    fi
  fi
done <"${BUILD_SCRIPT}"

# 记录真实 dist 内容指纹（路径+内容），确保测试不改动它
real_fingerprint=""
if [[ -d "${REAL_DIST}" ]]; then
  real_fingerprint="$(content_tree_hash "${REAL_DIST}")"
fi
real_backup_fingerprint=""
if [[ -d "${REAL_BACKUP}" ]]; then
  real_backup_fingerprint="$(content_tree_hash "${REAL_BACKUP}")"
fi

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/core-build-web-admin-contract.XXXXXX")"
trap 'rm -rf -- "${tmp_dir}"' EXIT

fake_admin="${tmp_dir}/admin"
fake_dist="${tmp_dir}/target"

seed_full_dist() {
  local dest="$1"
  rm -rf "${dest}/dist"
  mkdir -p "${dest}/dist/scripts" "${dest}/dist/main/:s/:c"
  printf '<html>ok</html>\n' >"${dest}/dist/index.html"
  printf '/* loading */\n' >"${dest}/dist/scripts/loading.js"
  printf 'console.log("umi");\n' >"${dest}/dist/umi.deadbeef.js"
  printf 'map-should-be-stripped\n' >"${dest}/dist/umi.deadbeef.js.map"
  printf 'stale-map\n' >"${dest}/dist/extra.map"
  # 非法动态路由静态页：构建后必须剔除
  printf '<html>param route</html>\n' >"${dest}/dist/main/:s/:c/index.html"
}

commit_admin() {
  local msg="$1"
  git -C "${fake_admin}" add -A
  # 允许 empty 以外的提交
  if test -n "$(git -C "${fake_admin}" status --porcelain)"; then
    git -C "${fake_admin}" commit -q -m "${msg}"
  fi
}

# 初始化假 git 仓库
mkdir -p "${fake_admin}"
git -C "${fake_admin}" init -q
git -C "${fake_admin}" config user.email "contract@test.local"
git -C "${fake_admin}" config user.name "contract"
seed_full_dist "${fake_admin}"
commit_admin "seed dist"
frontend_commit="$(git -C "${fake_admin}" rev-parse HEAD)"

run_build() {
  local out="$1"
  shift
  local st=0
  # 勿在函数内 set -e，否则非零 return 会在调用方误杀脚本
  env "$@" bash "${BUILD_SCRIPT}" >"${out}" 2>&1 || st=$?
  return "${st}"
}

# --- 0) 证明「仅 mv 已跟踪文件」会假绿（先脏树，而非缺产物）---
seed_full_dist "${fake_admin}"
commit_admin "reset full for false-green demo"
mv "${fake_admin}/dist/index.html" "${fake_admin}/dist/index.html.bak"
set +e
CORE_WEB_ADMIN_DIR="${fake_admin}" \
  CORE_WEB_DIST_DIR="${fake_dist}" \
  CORE_WEB_SKIP_NPM=1 \
  bash "${BUILD_SCRIPT}" >"${tmp_dir}/false_green.out" 2>&1
fg_status=$?
set -e
[[ "${fg_status}" -ne 0 ]] || fail "mv 缺 index 应失败"
grep -q '不干净' "${tmp_dir}/false_green.out" || fail "旧式 mv 应先触发脏树门禁（假绿证据）"
if grep -q '缺少 .*index.html' "${tmp_dir}/false_green.out"; then
  fail "旧式 mv 不应到达缺 index 分支（否则无法证明假绿）"
fi
mv "${fake_admin}/dist/index.html.bak" "${fake_admin}/dist/index.html"
git -C "${fake_admin}" checkout -q -- dist 2>/dev/null || true
git -C "${fake_admin}" clean -fdq
seed_full_dist "${fake_admin}"
commit_admin "restore after false-green"

# --- 1a) tracked 脏工作树拒绝 ---
echo dirty >>"${fake_admin}/dist/index.html"
set +e
run_build "${tmp_dir}/dirty.out" \
  CORE_WEB_ADMIN_DIR="${fake_admin}" \
  CORE_WEB_DIST_DIR="${fake_dist}" \
  CORE_WEB_SKIP_NPM=1
dirty_status=$?
set -e
[[ "${dirty_status}" -ne 0 ]] || fail "脏工作树应失败"
grep -q '不干净' "${tmp_dir}/dirty.out" || fail "脏工作树应提示不干净: $(cat "${tmp_dir}/dirty.out")"
git -C "${fake_admin}" checkout -q -- dist/index.html

# --- 1b) 未跟踪源码文件必须拒绝（完整 porcelain，不得放宽）---
mkdir -p "${fake_dist}"
# 干净树先建 target 基线
run_build "${tmp_dir}/untracked_ok_baseline.out" \
  CORE_WEB_ADMIN_DIR="${fake_admin}" \
  CORE_WEB_DIST_DIR="${fake_dist}" \
  CORE_WEB_SKIP_NPM=1 || fail "无未跟踪时应可构建基线: $(cat "${tmp_dir}/untracked_ok_baseline.out")"
untracked_target_fp_before="$(content_tree_hash "${fake_dist}")"
untracked_info_before="$(cat "${fake_dist}/build-info.json")"
fp_before_untracked="$(content_tree_hash "${REAL_DIST}")"
mkdir -p "${fake_admin}/src"
printf 'export const leak = 1;\n' >"${fake_admin}/src/uncommitted.ts"
set +e
run_build "${tmp_dir}/untracked_src.out" \
  CORE_WEB_ADMIN_DIR="${fake_admin}" \
  CORE_WEB_DIST_DIR="${fake_dist}" \
  CORE_WEB_SKIP_NPM=1
ut_status=$?
set -e
[[ "${ut_status}" -ne 0 ]] || fail "未跟踪源码存在时应失败"
grep -q '不干净' "${tmp_dir}/untracked_src.out" || fail "未跟踪源码应提示工作树不干净: $(cat "${tmp_dir}/untracked_src.out")"
untracked_target_fp_after="$(content_tree_hash "${fake_dist}")"
untracked_info_after="$(cat "${fake_dist}/build-info.json")"
[[ "${untracked_target_fp_before}" == "${untracked_target_fp_after}" ]] || fail "未跟踪失败后 fake target 应不变"
[[ "${untracked_info_before}" == "${untracked_info_after}" ]] || fail "未跟踪失败后 build-info 应不变"
fp_after_untracked="$(content_tree_hash "${REAL_DIST}")"
[[ "${fp_before_untracked}" == "${fp_after_untracked}" ]] || fail "未跟踪源码测试改动了真实 dist"
rm -f "${fake_admin}/src/uncommitted.ts"
git -C "${fake_admin}" clean -fdq

# --- 2) 缺 index.html（git clean：提交删除状态）---
rm -f "${fake_admin}/dist/index.html"
commit_admin "remove index.html"
[[ -z "$(git -C "${fake_admin}" status --porcelain)" ]] || fail "缺 index 场景工作树应干净"
set +e
run_build "${tmp_dir}/noindex.out" \
  CORE_WEB_ADMIN_DIR="${fake_admin}" \
  CORE_WEB_DIST_DIR="${fake_dist}" \
  CORE_WEB_SKIP_NPM=1
noindex_status=$?
set -e
[[ "${noindex_status}" -ne 0 ]] || fail "缺 index.html 应失败"
grep -q '缺少 .*index.html' "${tmp_dir}/noindex.out" || fail "缺 index 应报告缺少 index.html，输出: $(cat "${tmp_dir}/noindex.out")"
grep -q '不干净' "${tmp_dir}/noindex.out" && fail "缺 index 场景不应报脏树"
seed_full_dist "${fake_admin}"
commit_admin "restore after noindex"

# --- 3) 缺 scripts/loading.js（git clean）---
rm -f "${fake_admin}/dist/scripts/loading.js"
commit_admin "remove loading.js"
[[ -z "$(git -C "${fake_admin}" status --porcelain)" ]] || fail "缺 loading 场景工作树应干净"
set +e
run_build "${tmp_dir}/noload.out" \
  CORE_WEB_ADMIN_DIR="${fake_admin}" \
  CORE_WEB_DIST_DIR="${fake_dist}" \
  CORE_WEB_SKIP_NPM=1
noload_status=$?
set -e
[[ "${noload_status}" -ne 0 ]] || fail "缺 loading.js 应失败"
grep -q '缺少 .*loading.js' "${tmp_dir}/noload.out" || fail "缺 loading 应报告缺少 loading.js"
grep -q '不干净' "${tmp_dir}/noload.out" && fail "缺 loading 场景不应报脏树"
seed_full_dist "${fake_admin}"
commit_admin "restore after noload"

# --- 4) 缺 umi.*.js（git clean）---
rm -f "${fake_admin}/dist/umi.deadbeef.js"
commit_admin "remove umi js"
[[ -z "$(git -C "${fake_admin}" status --porcelain)" ]] || fail "缺 umi 场景工作树应干净"
set +e
run_build "${tmp_dir}/noumi.out" \
  CORE_WEB_ADMIN_DIR="${fake_admin}" \
  CORE_WEB_DIST_DIR="${fake_dist}" \
  CORE_WEB_SKIP_NPM=1
noumi_status=$?
set -e
[[ "${noumi_status}" -ne 0 ]] || fail "缺 umi.*.js 应失败"
grep -q '缺少 umi' "${tmp_dir}/noumi.out" || fail "缺 umi 应报告缺少 umi 产物"
grep -q '不干净' "${tmp_dir}/noumi.out" && fail "缺 umi 场景不应报脏树"
seed_full_dist "${fake_admin}"
commit_admin "restore after noumi"
frontend_commit="$(git -C "${fake_admin}" rev-parse HEAD)"

# --- 5) SKIP_NPM 必须双覆盖且非默认真实路径 ---
set +e
run_build "${tmp_dir}/skip_no_override.out" CORE_WEB_SKIP_NPM=1
skip1=$?
set -e
[[ "${skip1}" -ne 0 ]] || fail "仅 SKIP_NPM 无覆盖应失败"
grep -Eq 'CORE_WEB_SKIP_NPM|显式设置' "${tmp_dir}/skip_no_override.out" || fail "应提示需双覆盖"

set +e
run_build "${tmp_dir}/skip_default_target.out" \
  CORE_WEB_ADMIN_DIR="${fake_admin}" \
  CORE_WEB_SKIP_NPM=1
skip2=$?
set -e
[[ "${skip2}" -ne 0 ]] || fail "SKIP_NPM 缺 DIST 覆盖应失败"

set +e
run_build "${tmp_dir}/skip_real_admin.out" \
  CORE_WEB_ADMIN_DIR="${ROOT}/web/admin" \
  CORE_WEB_DIST_DIR="${fake_dist}" \
  CORE_WEB_SKIP_NPM=1
skip3=$?
set -e
[[ "${skip3}" -ne 0 ]] || fail "SKIP_NPM 使用默认 admin 应失败"
grep -q '默认 web/admin' "${tmp_dir}/skip_real_admin.out" || fail "应拒绝默认 admin"

set +e
run_build "${tmp_dir}/skip_real_dist.out" \
  CORE_WEB_ADMIN_DIR="${fake_admin}" \
  CORE_WEB_DIST_DIR="${ROOT}/pkg/server/run/dist" \
  CORE_WEB_SKIP_NPM=1
skip4=$?
set -e
[[ "${skip4}" -ne 0 ]] || fail "SKIP_NPM 使用默认 dist 应失败"
grep -q '默认 pkg/server/run/dist' "${tmp_dir}/skip_real_dist.out" || fail "应拒绝默认 dist"

# --- 5b) 生产模式禁止路径覆盖（无 SKIP_NPM）---
fp_before_prod="$(content_tree_hash "${REAL_DIST}")"
set +e
run_build "${tmp_dir}/prod_override_admin.out" \
  CORE_WEB_ADMIN_DIR="${fake_admin}"
prod1=$?
set -e
[[ "${prod1}" -ne 0 ]] || fail "生产模式设置 CORE_WEB_ADMIN_DIR 应失败"
grep -q '生产模式禁止' "${tmp_dir}/prod_override_admin.out" || fail "应提示生产模式禁止覆盖"

set +e
run_build "${tmp_dir}/prod_override_dist.out" \
  CORE_WEB_DIST_DIR="${fake_dist}"
prod2=$?
set -e
[[ "${prod2}" -ne 0 ]] || fail "生产模式设置 CORE_WEB_DIST_DIR 应失败"
grep -q '生产模式禁止' "${tmp_dir}/prod_override_dist.out" || fail "应提示生产模式禁止 DIST 覆盖"

set +e
run_build "${tmp_dir}/prod_override_both.out" \
  CORE_WEB_ADMIN_DIR="${fake_admin}" \
  CORE_WEB_DIST_DIR="${fake_dist}"
prod3=$?
set -e
[[ "${prod3}" -ne 0 ]] || fail "生产模式双覆盖应失败"
grep -q '生产模式禁止' "${tmp_dir}/prod_override_both.out" || fail "双覆盖应提示生产模式禁止"
fp_after_prod="$(content_tree_hash "${REAL_DIST}")"
[[ "${fp_before_prod}" == "${fp_after_prod}" ]] || fail "生产覆盖拒绝测试改动了真实 dist"

# --- 5c) 生产模式设置 DIRTY_MARKER 必须在 npm 前失败，且不得写 marker / 改真实 dist ---
printf 'prod-marker-v1\n' >"${tmp_dir}/prod_marker.txt"
prod_marker_before="$(shasum -a 256 "${tmp_dir}/prod_marker.txt" | awk '{print $1}')"
fp_before_marker="$(content_tree_hash "${REAL_DIST}")"
set +e
run_build "${tmp_dir}/prod_dirty_marker.out" \
  CORE_WEB_TEST_DIRTY_MARKER="${tmp_dir}/prod_marker.txt"
pdm=$?
set -e
[[ "${pdm}" -ne 0 ]] || fail "生产模式设置 CORE_WEB_TEST_DIRTY_MARKER 应失败"
grep -q '生产模式禁止' "${tmp_dir}/prod_dirty_marker.out" || fail "应提示生产模式禁止 DIRTY_MARKER"
prod_marker_after="$(shasum -a 256 "${tmp_dir}/prod_marker.txt" | awk '{print $1}')"
[[ "${prod_marker_before}" == "${prod_marker_after}" ]] || fail "生产拒绝 DIRTY_MARKER 时不得写入 marker 文件"
fp_after_marker="$(content_tree_hash "${REAL_DIST}")"
[[ "${fp_before_marker}" == "${fp_after_marker}" ]] || fail "生产 DIRTY_MARKER 拒绝测试改动了真实 dist"
grep -q '运行前端依赖' "${tmp_dir}/prod_dirty_marker.out" && fail "生产 DIRTY_MARKER 拒绝不得进入依赖安装"

# --- 5d) DIRTY_MARKER 拒绝 admin 外路径（SKIP 模式）---
set +e
run_build "${tmp_dir}/marker_outside.out" \
  CORE_WEB_ADMIN_DIR="${fake_admin}" \
  CORE_WEB_DIST_DIR="${fake_dist}" \
  CORE_WEB_SKIP_NPM=1 \
  CORE_WEB_TEST_DIRTY_MARKER="${tmp_dir}/outside.txt"
mo=$?
set -e
[[ "${mo}" -ne 0 ]] || fail "admin 外 DIRTY_MARKER 应失败"
# 文件可能不存在 → 普通文件检查；或存在但在 admin 外
grep -Eq 'DIRTY_MARKER|admin 目录内|普通文件' "${tmp_dir}/marker_outside.out" || fail "应拒绝 admin 外 marker"

# --- 5e) DIRTY_MARKER + 未跟踪 symlink：完整 dirty gate 或 marker 校验均须拒绝（不放宽工作树）---
seed_full_dist "${fake_admin}"
printf 'tracked\n' >"${fake_admin}/tracked.txt"
commit_admin "add tracked for dirty-after"
ln -sfn tracked.txt "${fake_admin}/tracked.link"
set +e
run_build "${tmp_dir}/marker_symlink.out" \
  CORE_WEB_ADMIN_DIR="${fake_admin}" \
  CORE_WEB_DIST_DIR="${fake_dist}" \
  CORE_WEB_SKIP_NPM=1 \
  CORE_WEB_TEST_DIRTY_MARKER="${fake_admin}/tracked.link"
ms=$?
set -e
[[ "${ms}" -ne 0 ]] || fail "符号链接 DIRTY_MARKER 应失败"
# 未跟踪 symlink 会先触发完整 porcelain 不干净；或到达 DIRTY_MARKER 专用拒绝
grep -Eq '不干净|符号链接|DIRTY_MARKER' "${tmp_dir}/marker_symlink.out" || fail "应拒绝 symlink/脏树: $(cat "${tmp_dir}/marker_symlink.out")"
rm -f "${fake_admin}/tracked.link"

# --- 5f) DIRTY_MARKER 指向未跟踪文件：完整 dirty gate 拒绝（不放宽）---
printf 'untracked\n' >"${fake_admin}/untracked.txt"
set +e
run_build "${tmp_dir}/marker_untracked.out" \
  CORE_WEB_ADMIN_DIR="${fake_admin}" \
  CORE_WEB_DIST_DIR="${fake_dist}" \
  CORE_WEB_SKIP_NPM=1 \
  CORE_WEB_TEST_DIRTY_MARKER="${fake_admin}/untracked.txt"
mu=$?
set -e
[[ "${mu}" -ne 0 ]] || fail "未跟踪 DIRTY_MARKER 应失败"
grep -Eq '不干净|已跟踪|DIRTY_MARKER' "${tmp_dir}/marker_untracked.out" || fail "应拒绝未跟踪/脏树: $(cat "${tmp_dir}/marker_untracked.out")"
rm -f "${fake_admin}/untracked.txt"
git -C "${fake_admin}" checkout -q -- . 2>/dev/null || true
git -C "${fake_admin}" clean -fdq

# --- 5g) 构建后 tracked 变脏必须失败，且 fake target 未被本次同步 ---
seed_full_dist "${fake_admin}"
printf 'tracked\n' >"${fake_admin}/tracked.txt"
commit_admin "add tracked for dirty-after-npm"
# 先用成功构建建立目标基线
mkdir -p "${fake_dist}"
run_build "${tmp_dir}/dirty_baseline.out" \
  CORE_WEB_ADMIN_DIR="${fake_admin}" \
  CORE_WEB_DIST_DIR="${fake_dist}" \
  CORE_WEB_SKIP_NPM=1 || fail "dirty 基线构建应成功"
[[ -f "${fake_dist}/build-info.json" ]] || fail "基线应有 build-info"
target_fp_before="$(content_tree_hash "${fake_dist}")"
target_info_before="$(cat "${fake_dist}/build-info.json")"
target_mtime_before="$(stat -f %m "${fake_dist}/build-info.json" 2>/dev/null || stat -c %Y "${fake_dist}/build-info.json")"

set +e
run_build "${tmp_dir}/dirty_after.out" \
  CORE_WEB_ADMIN_DIR="${fake_admin}" \
  CORE_WEB_DIST_DIR="${fake_dist}" \
  CORE_WEB_SKIP_NPM=1 \
  CORE_WEB_TEST_DIRTY_MARKER="${fake_admin}/tracked.txt"
dirty_after=$?
set -e
[[ "${dirty_after}" -ne 0 ]] || fail "构建后 tracked 变脏应失败"
grep -q '构建步骤后' "${tmp_dir}/dirty_after.out" || grep -q '工作树不干净' "${tmp_dir}/dirty_after.out" || fail "应报告构建后工作树不干净"
# fake target 不得被本次同步更新
target_fp_after="$(content_tree_hash "${fake_dist}")"
target_info_after="$(cat "${fake_dist}/build-info.json")"
target_mtime_after="$(stat -f %m "${fake_dist}/build-info.json" 2>/dev/null || stat -c %Y "${fake_dist}/build-info.json")"
[[ "${target_fp_before}" == "${target_fp_after}" ]] || fail "dirty-after 失败后 fake target 内容指纹应不变"
[[ "${target_info_before}" == "${target_info_after}" ]] || fail "dirty-after 失败后 build-info 应不变"
[[ "${target_mtime_before}" == "${target_mtime_after}" ]] || fail "dirty-after 失败后 build-info mtime 应不变"
fp_after_dirty="$(content_tree_hash "${REAL_DIST}")"
[[ "${fp_before_prod}" == "${fp_after_dirty}" ]] || fail "dirty-after 测试改动了真实 dist"
seed_full_dist "${fake_admin}"
rm -f "${fake_admin}/tracked.txt"
commit_admin "restore after dirty-after"

# --- 6) 危险目标拒绝（不真实操作 / 或 HOME）---
set +e
run_build "${tmp_dir}/danger_root.out" \
  CORE_WEB_ADMIN_DIR="${fake_admin}" \
  CORE_WEB_DIST_DIR="/" \
  CORE_WEB_SKIP_NPM=1
d1=$?
set -e
[[ "${d1}" -ne 0 ]] || fail "目标 / 应拒绝"
grep -Eq '危险|拒绝' "${tmp_dir}/danger_root.out" || fail "目标 / 应报危险"

set +e
run_build "${tmp_dir}/danger_home.out" \
  CORE_WEB_ADMIN_DIR="${fake_admin}" \
  CORE_WEB_DIST_DIR="${HOME}" \
  CORE_WEB_SKIP_NPM=1
d2=$?
set -e
[[ "${d2}" -ne 0 ]] || fail "目标 HOME 应拒绝"
grep -Eq 'HOME|危险|拒绝' "${tmp_dir}/danger_home.out" || fail "目标 HOME 应报拒绝"

set +e
run_build "${tmp_dir}/danger_repo.out" \
  CORE_WEB_ADMIN_DIR="${fake_admin}" \
  CORE_WEB_DIST_DIR="${ROOT}" \
  CORE_WEB_SKIP_NPM=1
d3=$?
set -e
[[ "${d3}" -ne 0 ]] || fail "目标仓库根应拒绝"

# --- 7) 成功路径：stale 删除、map 排除、冒号路径剔除、build-info ---
seed_full_dist "${fake_admin}"
commit_admin "full seed before success"
frontend_commit="$(git -C "${fake_admin}" rev-parse HEAD)"
mkdir -p "${fake_dist}"
printf 'old\n' >"${fake_dist}/stale.js"
printf 'oldmap\n' >"${fake_dist}/stale.js.map"

run_build "${tmp_dir}/ok.out" \
  CORE_WEB_ADMIN_DIR="${fake_admin}" \
  CORE_WEB_DIST_DIR="${fake_dist}" \
  CORE_WEB_SKIP_NPM=1 || fail "成功路径应通过: $(cat "${tmp_dir}/ok.out")"

[[ -f "${fake_dist}/index.html" ]] || fail "目标应有 index.html"
[[ -f "${fake_dist}/scripts/loading.js" ]] || fail "目标应有 scripts/loading.js"
[[ -f "${fake_dist}/umi.deadbeef.js" ]] || fail "目标应有 umi 包"
[[ ! -e "${fake_dist}/stale.js" ]] || fail "目标 stale.js 应被 rsync --delete 删除"
[[ ! -e "${fake_dist}/stale.js.map" ]] || fail "目标 stale map 应删除"
map_count="$(find "${fake_dist}" -type f -name '*.map' | wc -l | tr -d ' ')"
[[ "${map_count}" == "0" ]] || fail "目标不应包含任何 .map，实际 ${map_count}"
[[ -f "${fake_dist}/build-info.json" ]] || fail "缺少 build-info.json"
if find "${fake_dist}" -path '*:*' | grep -q .; then
  fail "目标不应含冒号路径: $(find "${fake_dist}" -path '*:*' | tr '\n' ' ')"
fi
[[ ! -e "${fake_dist}/main/:s/:c/index.html" ]] || fail "应剔除 main/:s/:c/index.html"

# 已存在目标时，下一次发布必须将旧版本移入唯一备份目录。
printf 'v1-marker\n' >"${fake_admin}/dist/version.txt"
commit_admin "version one"
run_build "${tmp_dir}/backup_v1.out" \
  CORE_WEB_ADMIN_DIR="${fake_admin}" \
  CORE_WEB_DIST_DIR="${fake_dist}" \
  CORE_WEB_SKIP_NPM=1 || fail "备份测试第一版发布应成功"
[[ -f "${fake_dist}/version.txt" ]] || fail "第一版应发布 version.txt"
printf 'v2-marker\n' >"${fake_admin}/dist/version.txt"
commit_admin "version two"
run_build "${tmp_dir}/backup_v2.out" \
  CORE_WEB_ADMIN_DIR="${fake_admin}" \
  CORE_WEB_DIST_DIR="${fake_dist}" \
  CORE_WEB_SKIP_NPM=1 || fail "备份测试第二版发布应成功"
grep -q '^v2-marker$' "${fake_dist}/version.txt" || fail "当前 dist 应为第二版"
grep -q '^v1-marker$' "${fake_dist}.backup/version.txt" || fail "dist.backup 应为上一版"
backup_count="$(find "${tmp_dir}" -maxdepth 1 -type d -name 'target.backup*' | wc -l | tr -d ' ')"
[[ "${backup_count}" == "1" ]] || fail "应仅保留一个 dist.backup，实际 ${backup_count}"
frontend_commit="$(git -C "${fake_admin}" rev-parse HEAD)"

info_commit="$(
  python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["frontend_commit"])' \
    "${fake_dist}/build-info.json"
)"
info_hash="$(
  python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["artifact_sha256"])' \
    "${fake_dist}/build-info.json"
)"
[[ "${info_commit}" == "${frontend_commit}" ]] || fail "frontend_commit 应等于子模块 HEAD"

expected_hash="$(
  cd "${fake_dist}"
  find . -type f ! -name build-info.json -print |
    LC_ALL=C sort |
    while IFS= read -r file; do
      shasum -a 256 "${file}"
    done |
    shasum -a 256 |
    awk '{print $1}'
)"
[[ "${info_hash}" == "${expected_hash}" ]] || fail "artifact_sha256 不可复算: got=${info_hash} want=${expected_hash}"

# hash 稳定
run_build "${tmp_dir}/ok2.out" \
  CORE_WEB_ADMIN_DIR="${fake_admin}" \
  CORE_WEB_DIST_DIR="${fake_dist}" \
  CORE_WEB_SKIP_NPM=1 || fail "第二次成功路径失败"
info_hash2="$(
  python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["artifact_sha256"])' \
    "${fake_dist}/build-info.json"
)"
[[ "${info_hash2}" == "${info_hash}" ]] || fail "相同产物 artifact_sha256 应稳定"

# --- 8) 内容指纹：同名文件内容变化必须改变 REAL_DIST 风格哈希 ---
probe="${tmp_dir}/probe_tree"
mkdir -p "${probe}"
printf 'v1\n' >"${probe}/a.js"
h1="$(content_tree_hash "${probe}")"
printf 'v2\n' >"${probe}/a.js"
h2="$(content_tree_hash "${probe}")"
[[ "${h1}" != "${h2}" ]] || fail "内容变化应改变 content_tree_hash"

# 真实 dist 未被测试改动（内容+路径）
if [[ -n "${real_fingerprint}" ]]; then
  after_fingerprint="$(content_tree_hash "${REAL_DIST}")"
  [[ "${after_fingerprint}" == "${real_fingerprint}" ]] || fail "契约测试改动了真实 pkg/server/run/dist 内容"
fi
if [[ -n "${real_backup_fingerprint}" ]]; then
  after_backup_fingerprint="$(content_tree_hash "${REAL_BACKUP}")"
  [[ "${after_backup_fingerprint}" == "${real_backup_fingerprint}" ]] || fail "契约测试改动了真实 pkg/server/run/dist.backup 内容"
fi

echo "build-web-admin 契约测试通过"
