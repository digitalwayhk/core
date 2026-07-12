#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GATE="${1:-}"

usage() {
  cat >&2 <<'EOF'
usage: scripts/ci.sh {required/quick|required/contracts|required/server-manage|required/race|observational/persistence|scheduled/stress|scheduled/integration}
EOF
}

case "$GATE" in
  required/quick)
    command=("$ROOT/scripts/test.sh" quick)
    ;;
  required/contracts)
    command=("$ROOT/scripts/test.sh" release-contract)
    ;;
  required/server-manage)
    command=(go test ./pkg/server/... ./service/manage/... -count=1 -timeout=10m)
    ;;
  required/race)
    command=("$ROOT/scripts/test.sh" concurrency-race)
    ;;
  observational/persistence)
    command=("$ROOT/scripts/test.sh" persistence-unit)
    ;;
  scheduled/stress)
    command=("$ROOT/scripts/test.sh" concurrency-stress)
    ;;
  scheduled/integration)
    command=("$ROOT/scripts/test.sh" integration-persistence)
    ;;
  *)
    usage
    exit 2
    ;;
esac

artifact_is_temporary=0
if [[ -n "${CI_ARTIFACT_DIR:-}" ]]; then
  artifact_dir="$CI_ARTIFACT_DIR"
else
  artifact_dir="$(mktemp -d "${TMPDIR:-/tmp}/digitalway-core-ci.XXXXXX")"
  artifact_is_temporary=1
fi
mkdir -p "$artifact_dir"
if [[ "$artifact_is_temporary" == "1" ]]; then
  trap 'rm -rf "$artifact_dir"' EXIT
fi

safe_gate="${GATE//\//-}"
log_file="$artifact_dir/${safe_gate}.log"
start_epoch="$(date +%s)"
go_version="$(go version 2>/dev/null || printf 'go unavailable')"
commit="$(git -C "$ROOT" rev-parse --verify HEAD 2>/dev/null || printf 'unknown')"

printf 'CI_GATE_START gate=%s commit=%s go=%s\n' "$GATE" "$commit" "$go_version"
set +e
(
  cd "$ROOT"
  "${command[@]}"
) 2>&1 | tee "$log_file"
status=${PIPESTATUS[0]}
set -e
end_epoch="$(date +%s)"
duration=$((end_epoch - start_epoch))
printf 'CI_GATE_END gate=%s exit_code=%d duration_seconds=%d log=%s\n' \
  "$GATE" "$status" "$duration" "$log_file"
exit "$status"
