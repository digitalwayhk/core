#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source_repo="${CORE_FUTURES_REPO:-}"
consumer_commit="${CORE_FUTURES_COMMIT:-203ff8eda53a9691d9409d3ee32aa5868fa1d61f}"
test_packages="${CORE_FUTURES_TEST_PACKAGES:-./gateway/api/... ./internal/pkg/services/worker/...}"
compile_packages="${CORE_FUTURES_COMPILE_PACKAGES:-./internal/pkg/services}"

if [[ -z "$source_repo" || ! -d "$source_repo/.git" && ! -f "$source_repo/.git" ]]; then
  echo "CONSUMER_SMOKE_STATUS=blocked reason=futures_repository_unavailable" >&2
  exit 3
fi
if ! git -C "$source_repo" cat-file -e "$consumer_commit^{commit}" 2>/dev/null; then
  echo "CONSUMER_SMOKE_STATUS=blocked reason=futures_commit_unavailable commit=$consumer_commit" >&2
  exit 3
fi

before_status="$(git -C "$source_repo" status --porcelain=v1 --untracked-files=all)"
tmp_root="$(mktemp -d "${TMPDIR:-/tmp}/digitalway-consumer-futures.XXXXXX")"
trap 'rm -rf "$tmp_root"' EXIT
consumer_dir="$tmp_root/futures"
mkdir -p "$consumer_dir"
git -C "$source_repo" archive "$consumer_commit" | tar -x -C "$consumer_dir"

go_mod_before="$(cksum "$consumer_dir/go.mod")"
go_sum_before="missing"
[[ ! -f "$consumer_dir/go.sum" ]] || go_sum_before="$(cksum "$consumer_dir/go.sum")"

work_file="$tmp_root/go.work"
(
  cd "$tmp_root"
  GOWORK=off go work init "$ROOT" "$consumer_dir"
)

read -r -a packages <<<"$test_packages"
read -r -a compile_targets <<<"$compile_packages"
(
  cd "$consumer_dir"
  GOWORK="$work_file" go test "${packages[@]}" -count=1 -timeout=10m
  GOWORK="$work_file" go test "${compile_targets[@]}" -run '^$' -count=1 -timeout=10m
)

[[ "$go_mod_before" == "$(cksum "$consumer_dir/go.mod")" ]] || {
  echo "消费方 smoke 修改了 go.mod" >&2
  exit 1
}
if [[ "$go_sum_before" == "missing" ]]; then
  [[ ! -f "$consumer_dir/go.sum" ]] || { echo "消费方 smoke 新增了 go.sum" >&2; exit 1; }
else
  [[ "$go_sum_before" == "$(cksum "$consumer_dir/go.sum")" ]] || {
    echo "消费方 smoke 修改了 go.sum" >&2
    exit 1
  }
fi

after_status="$(git -C "$source_repo" status --porcelain=v1 --untracked-files=all)"
[[ "$before_status" == "$after_status" ]] || {
  echo "消费方 smoke 修改了源 futures 工作树" >&2
  exit 1
}

echo "CONSUMER_SMOKE_STATUS=passed consumer=futures commit=$consumer_commit"
