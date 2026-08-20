#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GATE="${1:-}"

usage() {
  cat >&2 <<'EOF'
usage: scripts/ci.sh {required/quick|required/contracts|required/ai-skill|required/web-dist-sync|required/server-manage|required/simple-shop|required/race|observational/persistence|observational/casdoor-rbac|observational/shop-microservices|scheduled/stress|scheduled/integration|consumer/futures}
EOF
}

case "$GATE" in
  required/quick)
    command=("$ROOT/scripts/test.sh" quick)
    ;;
  required/contracts)
    command=("$ROOT/scripts/test.sh" release-contract)
    ;;
  required/ai-skill)
    command=("$ROOT/scripts/check-ai-skill.sh")
    ;;
  required/web-dist-sync)
    command=("$ROOT/scripts/check-web-dist-sync.sh")
    ;;
  required/server-manage)
    command=(go test ./pkg/server/... ./service/manage/... -count=1 -timeout=10m)
    ;;
  required/simple-shop)
    command=(go test ./examples/integration/01-simple-shop -count=1 -timeout=10m)
    ;;
  required/race)
    command=("$ROOT/scripts/test.sh" concurrency-race)
    ;;
  observational/persistence)
    command=("$ROOT/scripts/test.sh" persistence-unit)
    ;;
  observational/casdoor-rbac)
    command=("$ROOT/scripts/test.sh" integration-casdoor-rbac)
    ;;
  observational/shop-microservices)
    command=("$ROOT/scripts/test.sh" integration-shop-microservices)
    ;;
  scheduled/stress)
    command=("$ROOT/scripts/test.sh" concurrency-stress)
    ;;
  scheduled/integration)
    command=("$ROOT/scripts/test.sh" integration-persistence)
    ;;
  consumer/futures)
    command=("$ROOT/scripts/test-consumer-futures.sh")
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
os_version="$(uname -s)/$(uname -m)"
commit="$(git -C "$ROOT" rev-parse --verify HEAD 2>/dev/null || printf 'unknown')"
command_display="$(printf '%q ' "${command[@]}")"

printf 'CI_GATE_START gate=%s commit=%s os=%s go=%s command="%s"\n' \
  "$GATE" "$commit" "$os_version" "$go_version" "${command_display% }"
set +e
(
  cd "$ROOT"
  "${command[@]}"
) 2>&1 | tee "$log_file"
pipeline_status=("${PIPESTATUS[@]}")
command_status="${pipeline_status[0]}"
tee_status="${pipeline_status[1]}"
status="$command_status"
if [[ "$status" == "0" && "$tee_status" != "0" ]]; then
  status="$tee_status"
fi
set -e
end_epoch="$(date +%s)"
duration=$((end_epoch - start_epoch))
printf 'CI_GATE_END gate=%s exit_code=%d command_exit=%d tee_exit=%d duration_seconds=%d log="%s"\n' \
  "$GATE" "$status" "$command_status" "$tee_status" "$duration" "$log_file"
exit "$status"
