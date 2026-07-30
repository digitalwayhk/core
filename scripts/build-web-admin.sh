#!/usr/bin/env bash
# 从 web/admin 子模块生成可嵌入的前端产物，发布到 pkg/server/run/dist；
# 每次成功发布前将上一版保留为 pkg/server/run/dist.backup（仅保留一版）。
# 生产调用不得设置 CORE_WEB_SKIP_NPM=1，也不得覆盖 ADMIN/DIST 路径。
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
root_dir="$(cd -- "${script_dir}/.." && pwd)"
default_admin_dir="${root_dir}/web/admin"
default_target_dir="${root_dir}/pkg/server/run/dist"

# 规范化路径（不要求目标目录已存在）
canonical_path() {
  local p="$1"
  if [[ -d "${p}" ]]; then
    cd -- "${p}" && pwd -P
    return
  fi
  local parent base
  parent="$(dirname -- "${p}")"
  base="$(basename -- "${p}")"
  if [[ -d "${parent}" ]]; then
    echo "$(cd -- "${parent}" && pwd -P)/${base}"
  else
    echo "$(cd -- "${root_dir}" && pwd -P)/${p#${root_dir}/}"
  fi
}

# --- 路径覆盖仅允许契约测试模式（CORE_WEB_SKIP_NPM=1）---
if test "${CORE_WEB_SKIP_NPM:-0}" != "1"; then
  if [[ -n "${CORE_WEB_ADMIN_DIR:-}" || -n "${CORE_WEB_DIST_DIR:-}" ]]; then
    echo "生产模式禁止设置 CORE_WEB_ADMIN_DIR 或 CORE_WEB_DIST_DIR（仅 CORE_WEB_SKIP_NPM=1 契约测试可覆盖）" >&2
    exit 1
  fi
fi

admin_dir="${CORE_WEB_ADMIN_DIR:-${default_admin_dir}}"
target_dir="${CORE_WEB_DIST_DIR:-${default_target_dir}}"

if test "${CORE_WEB_SKIP_NPM:-0}" == "1"; then
  if [[ -z "${CORE_WEB_ADMIN_DIR:-}" || -z "${CORE_WEB_DIST_DIR:-}" ]]; then
    echo "CORE_WEB_SKIP_NPM=1 要求同时显式设置 CORE_WEB_ADMIN_DIR 与 CORE_WEB_DIST_DIR" >&2
    exit 1
  fi
  admin_canon="$(canonical_path "${admin_dir}")"
  target_canon_pre="$(canonical_path "${target_dir}")"
  default_admin_canon="$(canonical_path "${default_admin_dir}")"
  default_target_canon="$(canonical_path "${default_target_dir}")"
  if [[ "${admin_canon}" == "${default_admin_canon}" ]]; then
    echo "CORE_WEB_SKIP_NPM=1 禁止使用默认 web/admin 路径" >&2
    exit 1
  fi
  if [[ "${target_canon_pre}" == "${default_target_canon}" ]]; then
    echo "CORE_WEB_SKIP_NPM=1 禁止使用默认 pkg/server/run/dist 路径" >&2
    exit 1
  fi
fi

if [[ ! -d "${admin_dir}" ]]; then
  echo "web/admin 目录不存在: ${admin_dir}" >&2
  exit 1
fi

admin_dir="$(canonical_path "${admin_dir}")"
root_dir="$(canonical_path "${root_dir}")"

if [[ "${target_dir}" == "/" ]]; then
  target_dir="/"
elif [[ -d "${target_dir}" ]]; then
  target_dir="$(canonical_path "${target_dir}")"
else
  target_parent="$(dirname -- "${target_dir}")"
  target_base="$(basename -- "${target_dir}")"
  if [[ -d "${target_parent}" ]]; then
    target_dir="$(cd -- "${target_parent}" && pwd -P)/${target_base}"
  fi
fi

source_dir="${admin_dir}/dist"
default_target_canon="$(canonical_path "${default_target_dir}")"

assert_safe_target() {
  local t="$1"
  if [[ -z "${t}" || "${t}" == "/" ]]; then
    echo "拒绝危险目标路径: '${t}'" >&2
    exit 1
  fi
  local home_canon="/"
  if [[ -n "${HOME:-}" && -d "${HOME}" ]]; then
    home_canon="$(canonical_path "${HOME}")"
  fi
  if [[ "${t}" == "${home_canon}" ]]; then
    echo "拒绝以用户 HOME 作为目标: ${t}" >&2
    exit 1
  fi
  if [[ "${t}" == "${root_dir}" ]]; then
    echo "拒绝以仓库根目录作为目标: ${t}" >&2
    exit 1
  fi
  if [[ "${t}" == "${admin_dir}" ]]; then
    echo "拒绝以 web/admin 作为目标: ${t}" >&2
    exit 1
  fi
  if [[ "${t}" == "${source_dir}" ]]; then
    echo "拒绝以 admin/dist 作为目标: ${t}" >&2
    exit 1
  fi
  if [[ -z "${CORE_WEB_DIST_DIR:-}" ]]; then
    if [[ "${t}" != "${default_target_canon}" ]]; then
      echo "默认生产目标必须为 ${default_target_canon}，实际为 ${t}" >&2
      exit 1
    fi
  fi
}

assert_safe_target "${target_dir}"
target_parent="$(dirname -- "${target_dir}")"
backup_dir="${target_dir}.backup"

# 测试夹具 CORE_WEB_TEST_DIRTY_MARKER：生产模式一律拒绝（在 npm 前失败，绝不写入）
if [[ -n "${CORE_WEB_TEST_DIRTY_MARKER:-}" ]]; then
  if test "${CORE_WEB_SKIP_NPM:-0}" != "1"; then
    echo "生产模式禁止设置 CORE_WEB_TEST_DIRTY_MARKER" >&2
    exit 1
  fi
fi

# 完整工作树检查（含未跟踪源码；ignored 的 dist/.umi 不会出现在 porcelain 中）
if test -n "$(git -C "${admin_dir}" status --porcelain)"; then
  echo "web/admin 工作树不干净，拒绝生成嵌入产物" >&2
  exit 1
fi

if test "${CORE_WEB_SKIP_NPM:-0}" != "1"; then
  echo "运行前端依赖安装 / jest / tsc:auth / build …"
  # 仅依赖安装禁用 husky prepare：worktree/submodule 场景下 prepare 写父仓 git config
  # 会报 Operation not permitted，污染生产构建日志（即使 npm 最终 exit 0）。
  # jest / tsc:auth / build 不设 HUSKY=0，避免误伤其它生命周期脚本。
  if [[ -f "${admin_dir}/yarn.lock" ]]; then
    HUSKY=0 yarn --cwd "${admin_dir}" install --frozen-lockfile
  elif [[ -f "${admin_dir}/package-lock.json" ]]; then
    HUSKY=0 npm --prefix "${admin_dir}" ci
  else
    echo "web/admin 缺少 yarn.lock 或 package-lock.json，拒绝不可复现构建" >&2
    exit 1
  fi
  npm --prefix "${admin_dir}" run jest -- --runInBand
  npm --prefix "${admin_dir}" run tsc:auth
  npm --prefix "${admin_dir}" run build
else
  echo "CORE_WEB_SKIP_NPM=1：跳过 npm 步骤（仅契约测试）"
fi

# 契约测试夹具：仅 SKIP_NPM=1 + 双路径覆盖已通过时，污染 admin 内已跟踪普通文件
if [[ -n "${CORE_WEB_TEST_DIRTY_MARKER:-}" ]]; then
  marker_raw="${CORE_WEB_TEST_DIRTY_MARKER}"
  if [[ -L "${marker_raw}" ]]; then
    echo "CORE_WEB_TEST_DIRTY_MARKER 拒绝符号链接: ${marker_raw}" >&2
    exit 1
  fi
  if [[ ! -f "${marker_raw}" ]]; then
    echo "CORE_WEB_TEST_DIRTY_MARKER 必须是已存在的普通文件: ${marker_raw}" >&2
    exit 1
  fi
  marker_dir="$(cd -- "$(dirname -- "${marker_raw}")" && pwd -P)"
  marker_base="$(basename -- "${marker_raw}")"
  marker_canon="${marker_dir}/${marker_base}"
  case "${marker_canon}" in
    "${admin_dir}"/*) ;;
    *)
      echo "CORE_WEB_TEST_DIRTY_MARKER 必须位于 admin 目录内: ${marker_canon}" >&2
      exit 1
      ;;
  esac
  if [[ -L "${marker_canon}" || ! -f "${marker_canon}" ]]; then
    echo "CORE_WEB_TEST_DIRTY_MARKER 必须是普通非符号链接文件: ${marker_canon}" >&2
    exit 1
  fi
  rel="${marker_canon#${admin_dir}/}"
  if ! git -C "${admin_dir}" ls-files --error-unmatch -- "${rel}" >/dev/null 2>&1; then
    echo "CORE_WEB_TEST_DIRTY_MARKER 必须是 git 已跟踪文件: ${marker_canon}" >&2
    exit 1
  fi
  echo "dirty-after-npm" >>"${marker_canon}"
fi

# npm/构建后再次完整检查（含未跟踪源码；TOCTOU 防护）
if test -n "$(git -C "${admin_dir}" status --porcelain)"; then
  echo "npm/构建步骤后 web/admin 工作树不干净（含 tracked 变更），拒绝生成嵌入产物" >&2
  git -C "${admin_dir}" status --porcelain >&2 || true
  exit 1
fi

# frontend_commit 必须在最终 clean 检查之后读取
frontend_commit="$(git -C "${admin_dir}" rev-parse HEAD)"
echo "frontend_commit=${frontend_commit}"

if [[ ! -f "${source_dir}/index.html" ]]; then
  echo "缺少 ${source_dir}/index.html" >&2
  exit 1
fi
if [[ ! -f "${source_dir}/scripts/loading.js" ]]; then
  echo "缺少 ${source_dir}/scripts/loading.js" >&2
  exit 1
fi
if ! find "${source_dir}" -type f -name 'umi.*.js' -print -quit | grep -q .; then
  echo "缺少 umi.*.js 产物" >&2
  exit 1
fi

stage_dir="$(mktemp -d "${TMPDIR:-/tmp}/core-web-admin-stage.XXXXXX")"
publish_tmp=""
old_tmp=""
cleanup() {
  rm -rf -- "${stage_dir}"
  if [[ -n "${publish_tmp}" ]]; then
    rm -rf -- "${publish_tmp}"
  fi
  if [[ -n "${old_tmp}" ]]; then
    rm -rf -- "${old_tmp}"
  fi
}
trap cleanup EXIT

rsync -a --delete "${source_dir}/" "${stage_dir}/"
find "${stage_dir}" -type f -name '*.map' -delete

while IFS= read -r -d '' bad; do
  rm -rf -- "${bad}"
done < <(find "${stage_dir}" \( -type d -name ':*' -o -type f -path '*/:*/index.html' \) -print0 2>/dev/null || true)
while IFS= read -r -d '' bad; do
  rm -f -- "${bad}"
done < <(find "${stage_dir}" -type f -name '*:*' -print0 2>/dev/null || true)
find "${stage_dir}" -depth -type d -name ':*' -exec rm -rf {} + 2>/dev/null || true

test -f "${stage_dir}/index.html"
test -f "${stage_dir}/scripts/loading.js"
find "${stage_dir}" -type f -name 'umi.*.js' -print -quit | grep -q .
if find "${stage_dir}" -path '*:*' | grep -q .; then
  echo "stage 仍含冒号路径，拒绝同步" >&2
  find "${stage_dir}" -path '*:*' >&2 || true
  exit 1
fi

artifact_sha256="$(
  cd "${stage_dir}"
  find . -type f ! -name build-info.json -print |
    LC_ALL=C sort |
    while IFS= read -r file; do
      shasum -a 256 "${file}"
    done |
    shasum -a 256 |
    awk '{print $1}'
)"

printf '{\n  "frontend_commit": "%s",\n  "artifact_sha256": "%s"\n}\n' \
  "${frontend_commit}" "${artifact_sha256}" \
  >"${stage_dir}/build-info.json"

assert_safe_target "${target_dir}"
mkdir -p "${target_parent}"

# 先在目标父目录内准备完整新版本，再切换目录。这样构建/清理失败不会
# 半途改写当前 dist；发布成功后只保留一个可人工回滚的上一版本。
publish_tmp="$(mktemp -d "${target_parent}/.core-web-admin-publish.XXXXXX")"
rsync -a --delete "${stage_dir}/" "${publish_tmp}/dist/"
old_tmp="$(mktemp -d "${target_parent}/.core-web-admin-old.XXXXXX")"

if [[ -e "${target_dir}" || -L "${target_dir}" ]]; then
  mv -- "${target_dir}" "${old_tmp}/dist"
fi
if ! mv -- "${publish_tmp}/dist" "${target_dir}"; then
  if [[ -e "${old_tmp}/dist" || -L "${old_tmp}/dist" ]]; then
    mv -- "${old_tmp}/dist" "${target_dir}" || true
  fi
  echo "新前端产物切换失败，已尝试恢复当前 dist" >&2
  exit 1
fi

if [[ -e "${old_tmp}/dist" || -L "${old_tmp}/dist" ]]; then
  rm -rf -- "${backup_dir}"
  mv -- "${old_tmp}/dist" "${backup_dir}"
fi

echo "已同步到 ${target_dir}"
if [[ -e "${backup_dir}" ]]; then
  echo "上一版本备份到 ${backup_dir}（仅保留一版）"
fi
echo "artifact_sha256=${artifact_sha256}"
