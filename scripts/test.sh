#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

case "${1:-quick}" in
  quick)
    go vet ./pkg/server/...
    go test ./pkg/utils ./service/manage ./pkg/persistence/types -count=1
    ;;
  server)
    go vet ./pkg/server/...
    go test ./pkg/server/... -count=1
    ;;
  security)
    go test ./pkg/server/config ./pkg/server/safe/logto ./pkg/server/trans/rest ./pkg/utils -count=1
    ;;
  concurrency)
    go test -race \
      ./service/manage \
      ./pkg/server/api/manage \
      ./pkg/server/router \
      ./pkg/server/run \
      ./pkg/server/trans/rest \
      ./pkg/server/trans/websocket/melody \
      ./pkg/server/types \
      ./pkg/server/cluster \
      -count=1
    go test -race \
      ./pkg/server/run \
      ./pkg/server/router \
      ./pkg/server/cluster \
      ./pkg/server/trans/rest \
      ./pkg/server/trans/websocket/melody \
      ./pkg/server/types \
      -run 'Test.*Lifecycle|Test.*Concurrent.*Start.*Stop|Test.*Shutdown|Test.*Close|Test.*Cleanup' \
      -count=20
    ;;
  persistence-unit)
    go test ./pkg/persistence/... -count=1 -timeout=5m
    ;;
  integration-persistence)
    compose_file="$ROOT/docker-compose.integration.yml"
    lock_dir="${CORE_TEST_PERSISTENCE_LOCK_DIR:-${TMPDIR:-/tmp}/digitalway-core-persistence-integration.lock}"
    lock_acquired=0
    compose_started=0
    compose_up_pid=""
    compose_up_pgid=""
    test_pid=""
    test_pgid=""
    cleanup_timeout_seconds="${CORE_TEST_PERSISTENCE_CLEANUP_TIMEOUT_SECONDS:-30}"
    if [[ "${KEEP_CONTAINERS+x}" == "x" ]]; then
      keep_containers="$KEEP_CONTAINERS"
    else
      keep_containers="${CORE_TEST_KEEP_CONTAINERS:-0}"
    fi

    acquire_persistence_lock() {
      local owner_pid
      if mkdir "$lock_dir" 2>/dev/null; then
        lock_acquired=1
        echo "$$" >"$lock_dir/pid"
        return 0
      fi
      owner_pid="$(cat "$lock_dir/pid" 2>/dev/null || echo 未知)"
      echo "持久化集成测试锁已存在，owner PID: $owner_pid" >&2
      echo "请确认没有集成测试任务运行后，手动删除锁目录: $lock_dir" >&2
      return 1
    }

    release_persistence_lock() {
      local lock_owner
      if [[ "$lock_acquired" != "1" ]]; then
        return
      fi
      lock_owner="$(cat "$lock_dir/pid" 2>/dev/null || true)"
      if [[ -z "$lock_owner" || "$lock_owner" == "$$" ]]; then
        rm -rf "$lock_dir"
      fi
      lock_acquired=0
    }

    cleanup_persistence() {
      local down_pid down_pgid down_status watchdog_pid
      local timeout_marker="$lock_dir/compose-down-timeout"
      local complete_marker="$lock_dir/compose-down-complete"
      if [[ "$compose_started" != "1" ]]; then
        return
      fi
      if [[ "$keep_containers" == "1" ]]; then
        echo "保留持久化集成测试容器，便于调试"
        return
      fi
      rm -f "$timeout_marker" "$complete_marker"
      set -m
      (
        trap - INT TERM
        exec docker compose -f "$compose_file" --profile persistence down -v --remove-orphans
      ) &
      down_pid=$!
      down_pgid=$down_pid
      set +m
      perl -e '
        my ($pgid, $timeout_marker, $complete_marker, $timeout) = @ARGV;
        my $deadline = time + $timeout;
        while (!-e $complete_marker && time < $deadline) {
          select undef, undef, undef, 0.02;
        }
        exit 0 if -e $complete_marker;
        if (kill 0, -$pgid) {
          open my $fh, ">", $timeout_marker or die "无法写入 cleanup 超时标记: $!";
          print $fh "1\n";
          close $fh;
          print STDERR "compose-down 进程组 $pgid 超时，发送 TERM\n";
          kill "TERM", -$pgid;
        }
        select undef, undef, undef, 1;
        if (kill 0, -$pgid) {
          print STDERR "compose-down 进程组 $pgid 在 TERM 后仍存活，发送 KILL\n";
          kill "KILL", -$pgid;
        }
      ' "$down_pgid" "$timeout_marker" "$complete_marker" "$cleanup_timeout_seconds" &
      watchdog_pid=$!
      if wait "$down_pid" 2>/dev/null; then
        down_status=0
      else
        down_status=$?
      fi
      echo "1" >"$complete_marker"
      wait "$watchdog_pid" 2>/dev/null || true
      if ! ensure_process_group_stopped "$down_pgid"; then
        down_status=1
      fi
      if [[ -f "$timeout_marker" ]]; then
        rm -f "$timeout_marker" "$complete_marker"
        return 124
      fi
      rm -f "$complete_marker"
      return "$down_status"
    }

    finish_persistence() {
      local status=$?
      local cleanup_status=0
      trap - EXIT
      trap '' INT TERM
      set +e
      cleanup_persistence
      cleanup_status=$?
      release_persistence_lock
      if [[ "$status" -eq 0 && "$cleanup_status" -ne 0 ]]; then
        status=$cleanup_status
      fi
      exit "$status"
    }

    process_group_alive() {
      local pgid="$1"
      kill -0 -- "-$pgid" 2>/dev/null
    }

    wait_process_group_gone() {
      local pgid="$1"
      local attempts="${2:-50}"
      while process_group_alive "$pgid"; do
        attempts=$((attempts - 1))
        if [[ "$attempts" -eq 0 ]]; then
          return 1
        fi
        sleep 0.02
      done
    }

    ensure_process_group_stopped() {
      local pgid="$1"
      if wait_process_group_gone "$pgid" 10; then
        return 0
      fi
      echo "测试进程组 $pgid 未按时退出，发送 TERM" >&2
      kill -s TERM -- "-$pgid" 2>/dev/null || true
      if wait_process_group_gone "$pgid" 50; then
        return 1
      fi
      echo "测试进程组 $pgid 在 TERM 后仍存活，发送 KILL" >&2
      kill -s KILL -- "-$pgid" 2>/dev/null || true
      wait_process_group_gone "$pgid" 50
      return 1
    }

    stop_managed_process_group() {
      local signal="$1"
      local pid="$2"
      local pgid="$3"
      local label="$4"
      local watchdog_pid=""
      local watchdog_marker="$lock_dir/${label}-watchdog-escalated"
      managed_stop_failed=0
      if [[ -n "$pgid" ]] && process_group_alive "$pgid"; then
        rm -f "$watchdog_marker"
        kill -s "$signal" -- "-$pgid" 2>/dev/null || true
        (
          sleep 1
          if process_group_alive "$pgid"; then
            echo "1" >"$watchdog_marker"
            kill -s TERM -- "-$pgid" 2>/dev/null || true
          fi
          sleep 1
          if process_group_alive "$pgid"; then
            kill -s KILL -- "-$pgid" 2>/dev/null || true
          fi
        ) &
        watchdog_pid=$!
      fi
      if [[ -n "$pid" ]]; then
        wait "$pid" 2>/dev/null || true
      fi
      if [[ -n "$watchdog_pid" ]]; then
        kill "$watchdog_pid" 2>/dev/null || true
        wait "$watchdog_pid" 2>/dev/null || true
      fi
      if [[ -n "$pgid" ]] && ! ensure_process_group_stopped "$pgid"; then
        managed_stop_failed=1
      fi
      if [[ -f "$watchdog_marker" ]]; then
        echo "$label 进程组 $pgid 已升级终止" >&2
        rm -f "$watchdog_marker"
      fi
    }

    forward_persistence_signal() {
      local signal="$1"
      local status="$2"
      trap '' INT TERM
      if [[ -n "$compose_up_pid" || -n "$compose_up_pgid" ]]; then
        stop_managed_process_group "$signal" "$compose_up_pid" "$compose_up_pgid" "compose-up"
        if [[ "$managed_stop_failed" == "1" ]]; then
          status=1
        fi
        compose_up_pid=""
        compose_up_pgid=""
      fi
      if [[ -n "$test_pid" || -n "$test_pgid" ]]; then
        stop_managed_process_group "$signal" "$test_pid" "$test_pgid" "go-test"
        if [[ "$managed_stop_failed" == "1" ]]; then
          status=1
        fi
      fi
      test_pid=""
      test_pgid=""
      exit "$status"
    }

    trap finish_persistence EXIT
    trap 'forward_persistence_signal INT 130' INT
    trap 'forward_persistence_signal TERM 143' TERM
    acquire_persistence_lock

    compose_started=1
    set -m
    (
      trap - INT TERM
      exec docker compose -f "$compose_file" --profile persistence up -d --wait --wait-timeout 120
    ) &
    compose_up_pid=$!
    compose_up_pgid=$compose_up_pid
    set +m
    if wait "$compose_up_pid"; then
      compose_up_status=0
    else
      compose_up_status=$?
    fi
    if ! ensure_process_group_stopped "$compose_up_pgid"; then
      compose_up_status=1
    fi
    compose_up_pid=""
    compose_up_pgid=""
    if [[ "$compose_up_status" -ne 0 ]]; then
      exit "$compose_up_status"
    fi
    set -m
    (
      trap - INT TERM
      exec env \
        CORE_TEST_MYSQL=1 \
        CORE_TEST_MYSQL_HOST=127.0.0.1 \
        CORE_TEST_MYSQL_PORT=13306 \
        CORE_TEST_MYSQL_USER=core_test \
        CORE_TEST_MYSQL_PASSWORD=core_test_password \
        CORE_TEST_MYSQL_DATABASE=core_test \
        CORE_TEST_MONGODB=1 \
        CORE_TEST_MONGODB_HOST=127.0.0.1 \
        CORE_TEST_MONGODB_PORT=27018 \
        CORE_TEST_MONGODB_USER=core_test \
        CORE_TEST_MONGODB_PASSWORD=core_test_password \
        CORE_TEST_MONGODB_DATABASE=core_test \
        CORE_TEST_CLICKHOUSE=1 \
        CORE_TEST_CLICKHOUSE_HOST=127.0.0.1 \
        CORE_TEST_CLICKHOUSE_PORT=19000 \
        CORE_TEST_CLICKHOUSE_USER=core_test \
        CORE_TEST_CLICKHOUSE_PASSWORD=core_test_password \
        CORE_TEST_CLICKHOUSE_DATABASE=core_test \
        go test -tags=integration \
        ./pkg/persistence/database/oltp \
        ./pkg/persistence/database/nosql \
        ./pkg/persistence/database/olap \
        -run 'Test(MySQL|Mongo|ClickHouse)Integration_DriverContract' \
        -count=1 -timeout=5m
    ) &
    test_pid=$!
    test_pgid=$test_pid
    set +m
    if wait "$test_pid"; then
      test_status=0
    else
      test_status=$?
    fi
    if ! ensure_process_group_stopped "$test_pgid"; then
      test_status=1
    fi
    test_pid=""
    test_pgid=""
    exit "$test_status"
    ;;
  integration-local)
    CORE_TEST_CLUSTER_LOCAL=1 go test -tags=integration ./tests/integration -run TestClusterLocal -count=1
    ;;
  integration-external)
    CORE_TEST_ETCD=1 \
    ETCD_ENDPOINTS="${ETCD_ENDPOINTS:-127.0.0.1:2379}" \
    CORE_TEST_CONSUL=1 \
    CONSUL_HTTP_ADDR="${CONSUL_HTTP_ADDR:-127.0.0.1:8500}" \
    CORE_TEST_REDIS_STREAM=1 \
    CORE_TEST_REDIS_ADDR="${CORE_TEST_REDIS_ADDR:-127.0.0.1:6379}" \
    CORE_TEST_NATS=1 \
    CORE_TEST_NATS_URL="${CORE_TEST_NATS_URL:-nats://127.0.0.1:4222}" \
    go test -tags=integration ./tests/integration -run 'TestClusterEtcd|TestClusterConsul|TestMQ' -count=1
    ;;
  all)
    "$0" quick
    "$0" server
    "$0" concurrency
    "$0" integration-local
    "$0" integration-external
    ;;
  *)
    echo "usage: scripts/test.sh {quick|server|security|concurrency|persistence-unit|integration-local|integration-external|integration-persistence|all}" >&2
    exit 2
    ;;
esac
