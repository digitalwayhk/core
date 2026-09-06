#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
compose_file="$ROOT/docker-compose.integration.yml"
project_name="${CORE_TEST_EXTERNAL_PROJECT_NAME:-digitalway-core-external-$$}"
lock_dir="${CORE_TEST_EXTERNAL_LOCK_DIR:-${TMPDIR:-/tmp}/digitalway-core-external-integration.lock}"
up_timeout="${CORE_TEST_EXTERNAL_UP_TIMEOUT_SECONDS:-600}"
cleanup_timeout="${CORE_TEST_EXTERNAL_CLEANUP_TIMEOUT_SECONDS:-60}"
lock_acquired=0
started=0

[[ "$project_name" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || { echo "外部集成 project name 无效: $project_name" >&2; exit 2; }

run_bounded() {
  local timeout="$1"
  shift
  perl -e '
    my ($timeout, @cmd) = @ARGV;
    my $pid = fork();
    die "fork 失败" unless defined $pid;
    if ($pid == 0) {
      setpgrp(0, 0);
      exec @cmd;
      die "exec 失败: $!";
    }
    $SIG{ALRM} = sub {
      kill "TERM", -$pid;
      select undef, undef, undef, 1;
      kill "KILL", -$pid;
      waitpid $pid, 0;
      exit 124;
    };
    alarm $timeout;
    waitpid $pid, 0;
    alarm 0;
    exit(($? & 127) ? 128 + ($? & 127) : $? >> 8);
  ' "$timeout" "$@"
}

capture_diagnostics() {
  [[ -n "${CI_ARTIFACT_DIR:-}" ]] || return 0
  mkdir -p "$CI_ARTIFACT_DIR"
  docker compose --project-name "$project_name" -f "$compose_file" ps >"$CI_ARTIFACT_DIR/compose-ps.log" 2>&1 || true
  docker compose --project-name "$project_name" -f "$compose_file" logs --no-color \
    >"$CI_ARTIFACT_DIR/compose.log" 2>&1 || true
}

run_mq_restart_phase() {
  local test_name="$1"
  CORE_TEST_REDIS_STREAM=1 \
  CORE_TEST_REDIS_ADDR="${CORE_TEST_REDIS_ADDR:-127.0.0.1:6379}" \
  CORE_TEST_NATS=1 \
  CORE_TEST_NATS_URL="${CORE_TEST_NATS_URL:-nats://127.0.0.1:4222}" \
  CORE_TEST_MQ_RESTART_TOKEN="$mq_restart_token" \
    go test -tags=integration ./tests/integration -run "^${test_name}$" -count=1
}

restart_broker() {
  local service="$1"
  run_bounded 120 docker compose --project-name "$project_name" -f "$compose_file" restart -t 20 "$service"
  run_bounded 120 docker compose --project-name "$project_name" -f "$compose_file" up -d --wait --wait-timeout 90 "$service"
}

finish() {
  local status=$?
  local cleanup_status=0
  trap - EXIT INT TERM
  set +e
  if [[ "$status" -ne 0 ]]; then
    capture_diagnostics
  fi
  if [[ "$started" == "1" && "${KEEP_CONTAINERS:-0}" != "1" ]]; then
    run_bounded "$cleanup_timeout" docker compose --project-name "$project_name" -f "$compose_file" down -v --remove-orphans
    cleanup_status=$?
  fi
  if [[ "$lock_acquired" == "1" ]]; then
    rm -rf "$lock_dir"
  fi
  if [[ "$status" -eq 0 && "$cleanup_status" -ne 0 ]]; then
    status=$cleanup_status
  fi
  exit "$status"
}

mkdir "$lock_dir" 2>/dev/null || {
  echo "外部集成测试锁已存在: $lock_dir" >&2
  exit 1
}
lock_acquired=1
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

started=1
run_bounded "$up_timeout" docker compose --project-name "$project_name" -f "$compose_file" up -d --wait --wait-timeout 120 etcd consul redis nats
"$ROOT/scripts/test.sh" integration-external

# Broker 重启必须由测试编排层显式执行；框架运行时代码不得获得 Docker 控制权。
mq_restart_token="$(date +%s)-$$"
run_mq_restart_phase TestMQRedisLifecycleBrokerRestartPrepare
restart_broker redis
run_mq_restart_phase TestMQRedisLifecycleBrokerRestartRecover
run_mq_restart_phase TestMQNATSLifecycleBrokerRestartPrepare
restart_broker nats
run_mq_restart_phase TestMQNATSLifecycleBrokerRestartRecover
