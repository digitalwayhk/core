#!/usr/bin/env bash

PUBLIC_API_TOOL_VERSION="v0.0.0-20260410095643-746e56fc9e2f"
PUBLIC_API_TOOL_PACKAGE="golang.org/x/exp/cmd/apidiff"
PUBLIC_API_PACKAGES_FILE="$ROOT/api/public-packages.txt"
PUBLIC_API_BASELINE_DIR="$ROOT/api/public-api"

public_api_baseline_name() {
  local package="$1"
  printf '%s.apidiff' "${package//\//_}"
}

run_apidiff() {
  local tool_dir tool_bin build_bin
  tool_dir="${CORE_PUBLIC_API_TOOL_CACHE:-${TMPDIR:-/tmp}/digitalway-core-public-api-tool}"
  tool_bin="$tool_dir/apidiff-${PUBLIC_API_TOOL_VERSION}"
  if [[ ! -x "$tool_bin" ]]; then
    mkdir -p "$tool_dir"
    build_bin="$(mktemp "$tool_dir/apidiff-build.XXXXXX")"
    if ! (cd "$ROOT/tools" && go build -o "$build_bin" "$PUBLIC_API_TOOL_PACKAGE"); then
      rm -f "$build_bin"
      return 1
    fi
    chmod +x "$build_bin"
    mv "$build_bin" "$tool_bin"
  fi
  (cd "$ROOT" && "$tool_bin" "$@")
}

compare_public_api() {
  local old="$1"
  local new="$2"
  local report incompatible
  report="$(run_apidiff "$old" "$new")"
  incompatible="$(run_apidiff -incompatible "$old" "$new")"
  if [[ -n "${incompatible//[[:space:]]/}" ]]; then
    printf '%s\n' "$incompatible" >&2
    return 1
  fi
  if [[ -n "${report//[[:space:]]/}" ]]; then
    printf '%s\n' "$report"
  fi
}

read_public_api_packages() {
  if [[ ! -s "$PUBLIC_API_PACKAGES_FILE" ]]; then
    echo "公共 API 包清单不存在或为空: $PUBLIC_API_PACKAGES_FILE" >&2
    return 1
  fi
  sed -e '/^[[:space:]]*#/d' -e '/^[[:space:]]*$/d' "$PUBLIC_API_PACKAGES_FILE"
}

verify_public_api_tool_version() {
  grep -Fq "golang.org/x/exp $PUBLIC_API_TOOL_VERSION" "$ROOT/tools/go.mod" || {
    echo "apidiff 工具版本未按预期锁定: $PUBLIC_API_TOOL_VERSION" >&2
    return 1
  }
}
