#!/usr/bin/env bash
# 校验已提交的内嵌前端产物与 web/admin 子模块指针一致。
#
# 背景：pkg/server/run/dist 是直接提交进仓库的前端产物，由 scripts/build-web-admin.sh
# 生成，构建时把当时的 web/admin HEAD 写进 dist/build-info.json 的 frontend_commit。
# 子模块指针前进而 dist 没重建时，服务内嵌的仍是旧前端，且没有任何运行时报错。
# scripts/test-build-web-admin.sh 只用合成 fixture 验证构建脚本行为，并明确断言真实
# dist 不被改动，因此看不到真实产物的漂移；这个脚本补的就是这个缺口。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}"

SUBMODULE_PATH="web/admin"
BUILD_INFO="pkg/server/run/dist/build-info.json"

fail() {
  echo "内嵌前端产物校验失败: $*" >&2
  exit 1
}

read -r -d '' PARSE_BUILD_INFO <<'PY' || true
import json
import sys

path = sys.argv[1]
try:
    with open(path, encoding="utf-8") as handle:
        info = json.load(handle)
except UnicodeDecodeError as exc:
    sys.exit("不是 UTF-8 文本: %s" % exc)
except json.JSONDecodeError as exc:
    sys.exit("不是合法 JSON: %s" % exc)

if not isinstance(info, dict):
    sys.exit("顶层不是 JSON 对象")
if "frontend_commit" not in info:
    sys.exit("缺少 frontend_commit 字段")

value = info["frontend_commit"]
if not isinstance(value, str) or not value.strip():
    sys.exit("frontend_commit 为空或不是字符串")

print(value.strip())
PY

command -v python3 >/dev/null 2>&1 ||
  fail "缺少 python3，无法解析 ${BUILD_INFO}"

git rev-parse --verify HEAD >/dev/null 2>&1 ||
  fail "无法解析当前仓库 HEAD，请在 core 仓库的有效检出中运行"

# 必须读 HEAD 里已提交的指针：子模块工作区可能被本地 checkout 移到别的 commit，
# 用 git -C web/admin rev-parse HEAD 会把已提交的漂移放过去。
tree_entry="$(git ls-tree HEAD -- "${SUBMODULE_PATH}" 2>/dev/null || true)"
if [[ -z "${tree_entry}" ]]; then
  fail "git ls-tree HEAD ${SUBMODULE_PATH} 无输出，HEAD 中没有该子模块条目；请确认检出完整且子模块已登记"
fi

read -r entry_mode entry_type submodule_commit _ <<<"${tree_entry}"
if [[ "${entry_mode}" != "160000" || "${entry_type}" != "commit" ]]; then
  fail "${SUBMODULE_PATH} 在 HEAD 中不是子模块条目（mode=${entry_mode} type=${entry_type}），无法取得前端指针"
fi
if [[ ! "${submodule_commit}" =~ ^[0-9a-f]{40}$ ]]; then
  fail "${SUBMODULE_PATH} 的子模块指针不是合法 commit：${submodule_commit}"
fi

if [[ ! -f "${BUILD_INFO}" ]]; then
  fail "缺少 ${BUILD_INFO}；内嵌前端产物不完整，请运行 ./scripts/build-web-admin.sh 重建并提交 dist"
fi

if ! info_commit="$(python3 -c "${PARSE_BUILD_INFO}" "${BUILD_INFO}" 2>&1)"; then
  fail "解析 ${BUILD_INFO} 失败：${info_commit}；请运行 ./scripts/build-web-admin.sh 重建并提交 dist"
fi

if [[ "${info_commit}" != "${submodule_commit}" ]]; then
  {
    echo "内嵌前端产物校验失败: dist 与 web/admin 子模块指针不一致"
    echo "  ${SUBMODULE_PATH} 已提交指针: ${submodule_commit}"
    echo "  ${BUILD_INFO} 的 frontend_commit: ${info_commit}"
    echo ""
    echo "服务内嵌的是 frontend_commit 对应的旧前端。请运行 ./scripts/build-web-admin.sh 重建并提交 dist，"
    echo "使 build-info.json 的 frontend_commit 与子模块指针一致（两者必须在同一个提交里同时更新）。"
  } >&2
  exit 1
fi

echo "内嵌前端产物校验通过：${SUBMODULE_PATH} 指针与 ${BUILD_INFO} 均为 ${submodule_commit}"
