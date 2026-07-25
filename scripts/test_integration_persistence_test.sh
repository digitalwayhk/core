#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/bin"
mkdir -p "$TMP/runtime"
export TMPDIR="$TMP/runtime"

cat >"$TMP/bin/docker" <<'EOF'
#!/usr/bin/env bash
echo "docker $*" >>"$TEST_TRACE"
if [[ " $* " == *" up "* && "${FAKE_DOCKER_UP_BLOCK:-0}" == "1" ]]; then
  up_pgid="$(perl -e 'print getpgrp(0)')"
  echo "docker-up-start $$" >>"$TEST_TRACE"
  echo "docker-up-pgid $up_pgid" >>"$TEST_TRACE"
  up_child=""
  handle_up_signal() {
    local signal="$1"
    local status="$2"
    echo "docker-up-signal $signal" >>"$TEST_TRACE"
    if [[ -n "$up_child" ]]; then
      wait "$up_child" 2>/dev/null || true
      echo "docker-up-grandchild-reaped $up_child" >>"$TEST_TRACE"
    fi
    exit "$status"
  }
  trap 'handle_up_signal INT 130' INT
  trap 'handle_up_signal TERM 143' TERM
  perl -e '
    my $ignore = $ENV{FAKE_DOCKER_UP_GRANDCHILD_IGNORE_SIGNAL} // "";
    $SIG{INT} = $ignore eq "INT" ? "IGNORE" : "DEFAULT";
    $SIG{TERM} = $ignore eq "TERM" ? "IGNORE" : "DEFAULT";
    if (open my $fh, ">>", $ENV{TEST_TRACE}) {
      print $fh "docker-up-grandchild-pgid ", getpgrp(0), "\n";
      close $fh;
    }
    exec "sleep", "300";
  ' &
  up_child=$!
  echo "docker-up-grandchild $up_child" >>"$TEST_TRACE"
  while :; do sleep 0.05; done
fi
if [[ " $* " == *" down "* ]]; then
  pgid="$(awk '/go-pgid/ {print $2; exit}' "$TEST_TRACE")"
  if [[ -n "$pgid" ]] && kill -0 -- "-$pgid" 2>/dev/null; then
    echo "cleanup-while-group-alive $pgid" >>"$TEST_TRACE"
    exit 9
  fi
  up_pgid="$(awk '/docker-up-pgid/ {print $2; exit}' "$TEST_TRACE")"
  if [[ -n "$up_pgid" ]] && kill -0 -- "-$up_pgid" 2>/dev/null; then
    echo "cleanup-while-compose-up-group-alive $up_pgid" >>"$TEST_TRACE"
    exit 10
  fi
  if [[ "${FAKE_DOCKER_DOWN_BLOCK:-0}" == "1" ]]; then
    down_pgid="$(perl -e 'print getpgrp(0)')"
    echo "docker-down-start $$" >>"$TEST_TRACE"
    echo "docker-down-pgid $down_pgid" >>"$TEST_TRACE"
    trap '' INT TERM
    perl -e '
      $SIG{INT} = "IGNORE";
      $SIG{TERM} = "IGNORE";
      if (open my $fh, ">>", $ENV{TEST_TRACE}) {
        print $fh "docker-down-grandchild-pgid ", getpgrp(0), "\n";
        close $fh;
      }
      exec "sleep", "300";
    ' &
    down_child=$!
    echo "docker-down-grandchild $down_child" >>"$TEST_TRACE"
    while :; do sleep 0.05; done
  fi
fi
EOF

cat >"$TMP/bin/go" <<'EOF'
#!/usr/bin/env bash
echo "go-start $$" >>"$TEST_TRACE"
go_pgid="$(perl -e 'print getpgrp(0)')"
echo "go-pgid $go_pgid" >>"$TEST_TRACE"
grandchild_pid=""
handle_signal() {
  local signal="$1"
  local status="$2"
  echo "go-signal $signal" >>"$TEST_TRACE"
  if [[ -n "$grandchild_pid" ]]; then
    wait "$grandchild_pid" 2>/dev/null || true
    echo "go-grandchild-reaped $grandchild_pid" >>"$TEST_TRACE"
  fi
  exit "$status"
}
trap 'handle_signal INT 130' INT
trap 'handle_signal TERM 143' TERM
if [[ "${FAKE_GO_BLOCK:-0}" == "1" ]]; then
  perl -e '
    my $ignore = $ENV{FAKE_GRANDCHILD_IGNORE_SIGNAL} // "";
    $SIG{INT} = $ignore eq "INT" ? "IGNORE" : "DEFAULT";
    $SIG{TERM} = $ignore eq "TERM" ? "IGNORE" : "DEFAULT";
    if (open my $fh, ">>", $ENV{TEST_TRACE}) {
      print $fh "go-grandchild-pgid ", getpgrp(0), "\n";
      close $fh;
    }
    exec "sleep", "300";
  ' &
  grandchild_pid=$!
  echo "go-grandchild $grandchild_pid" >>"$TEST_TRACE"
  if [[ "${FAKE_GO_LEADER_EXIT:-0}" == "1" ]]; then
    echo "go-leader-exit $$" >>"$TEST_TRACE"
    exit 0
  fi
  while :; do sleep 0.05; done
fi
exit "${FAKE_GO_EXIT:-0}"
EOF

cat >"$TMP/bin/mkdir" <<'EOF'
#!/usr/bin/env bash
echo "mkdir $*" >>"$TEST_TRACE"
exec /bin/mkdir "$@"
EOF

chmod +x "$TMP/bin/docker" "$TMP/bin/go" "$TMP/bin/mkdir"

wait_for_trace() {
  local file="$1"
  local pattern="$2"
  local attempts=100
  while ! grep -q "$pattern" "$file" 2>/dev/null; do
    attempts=$((attempts - 1))
    if [[ "$attempts" -eq 0 ]]; then
      echo "等待日志超时: $pattern" >&2
      return 1
    fi
    sleep 0.02
  done
}

run_case() {
  local name="$1"
  shift
  local trace="$TMP/$name.log"
  : >"$trace"
  env -u KEEP_CONTAINERS -u CORE_TEST_KEEP_CONTAINERS \
    PATH="$TMP/bin:$PATH" TEST_TRACE="$trace" "$@" \
    "$ROOT/scripts/test.sh" integration-persistence
}

wait_for_dead() {
  local pid="$1"
  local attempts=100
  while kill -0 "$pid" 2>/dev/null; do
    attempts=$((attempts - 1))
    if [[ "$attempts" -eq 0 ]]; then
      echo "进程未退出: $pid" >&2
      return 1
    fi
    sleep 0.02
  done
}

assert_order() {
  local file="$1"
  local first="$2"
  local second="$3"
  local first_line second_line
  first_line="$(grep -n "$first" "$file" | head -1 | cut -d: -f1)"
  second_line="$(grep -n "$second" "$file" | head -1 | cut -d: -f1)"
  if [[ -z "$first_line" || -z "$second_line" || "$first_line" -ge "$second_line" ]]; then
    echo "日志顺序错误: $first 应早于 $second" >&2
    return 1
  fi
}

run_case persistence_services_only
if ! grep -q ' up .* mysql mongodb clickhouse' "$TMP/persistence_services_only.log"; then
  echo "持久化门禁应只显式启动 mysql、mongodb、clickhouse" >&2
  exit 1
fi
if grep -q ' up .* redis' "$TMP/persistence_services_only.log"; then
  echo "持久化门禁不应启动无关 redis 服务" >&2
  exit 1
fi

run_case keep_primary KEEP_CONTAINERS=1 CORE_TEST_KEEP_CONTAINERS=0
if grep -q ' down ' "$TMP/keep_primary.log"; then
  echo "KEEP_CONTAINERS=1 时不应清理容器" >&2
  exit 1
fi

run_case primary_overrides_legacy KEEP_CONTAINERS=0 CORE_TEST_KEEP_CONTAINERS=1
if ! grep -q ' down ' "$TMP/primary_overrides_legacy.log"; then
  echo "KEEP_CONTAINERS=0 应优先于旧开关并执行清理" >&2
  exit 1
fi

run_case keep_legacy CORE_TEST_KEEP_CONTAINERS=1
if grep -q ' down ' "$TMP/keep_legacy.log"; then
  echo "CORE_TEST_KEEP_CONTAINERS=1 时不应清理容器" >&2
  exit 1
fi

set +e
run_case failed_test KEEP_CONTAINERS=0 FAKE_GO_EXIT=7
status=$?
set -e
if [[ "$status" -ne 7 ]]; then
  echo "Go 测试失败码应原样返回: got=$status want=7" >&2
  exit 1
fi
if ! grep -q ' down ' "$TMP/failed_test.log"; then
  echo "Go 测试失败后 cleanup trap 必须清理容器" >&2
  exit 1
fi

run_signal_case() {
  local signal="$1"
  local want_status="$2"
  local name="signal_${signal}"
  local trace="$TMP/$name.log"
  local lock="$TMP/$name.lock"
  : >"$trace"
  set +e
  env PATH="$TMP/bin:$PATH" TEST_TRACE="$trace" \
    CORE_TEST_PERSISTENCE_LOCK_DIR="$lock" FAKE_GO_BLOCK=1 \
    perl -e '
      my ($trace, $signal, @cmd) = @ARGV;
      my $pid = fork();
      die "fork 失败" unless defined $pid;
      if ($pid == 0) {
        $SIG{INT} = "DEFAULT";
        $SIG{TERM} = "DEFAULT";
        exec @cmd;
        die "exec 失败";
      }
      $SIG{ALRM} = sub {
        my $pgid = "";
        if (open my $fh, "<", $trace) {
          while (<$fh>) { $pgid = $1 if /go-pgid\s+(\d+)/; }
          close $fh;
        }
        kill "KILL", -$pgid if $pgid;
        kill "KILL", $pid;
        waitpid $pid, 0;
        exit 125;
      };
      alarm 5;
      my $ready = 0;
      for (1..200) {
        if (open my $fh, "<", $trace) {
          local $/;
          $ready = 1 if index(<$fh> // "", "go-grandchild-pgid") >= 0;
          close $fh;
        }
        last if $ready;
        select undef, undef, undef, 0.02;
      }
      if (!$ready) {
        kill "TERM", $pid;
        waitpid $pid, 0;
        die "等待进程组就绪超时";
      }
      kill $signal, $pid or die "发送信号失败";
      waitpid $pid, 0;
      alarm 0;
      exit(($? & 127) ? 128 + ($? & 127) : $? >> 8);
    ' "$trace" "$signal" "$ROOT/scripts/test.sh" integration-persistence
  local status=$?
  set -e
  if [[ "$status" -ne "$want_status" ]]; then
    echo "$signal 退出码错误: got=$status want=$want_status" >&2
    return 1
  fi
  wait_for_trace "$trace" "go-signal $signal"
  wait_for_trace "$trace" 'go-grandchild'
  wait_for_trace "$trace" ' down '
  assert_order "$trace" "go-signal $signal" ' down '
  if grep -q 'cleanup-while-group-alive' "$trace"; then
    echo "$signal cleanup 时测试进程组仍存活" >&2
    return 1
  fi
  local child_pid
  child_pid="$(awk '/go-start/ {print $2; exit}' "$trace")"
  local grandchild_pid
  grandchild_pid="$(awk '/go-grandchild/ {print $2; exit}' "$trace")"
  local leader_pgid grandchild_pgid
  leader_pgid="$(awk '/go-pgid/ {print $2; exit}' "$trace")"
  grandchild_pgid="$(awk '/go-grandchild-pgid/ {print $2; exit}' "$trace")"
  if [[ "$leader_pgid" != "$child_pid" || "$grandchild_pgid" != "$leader_pgid" ]]; then
    echo "$signal 进程组错误: child=$child_pid leader_pgid=$leader_pgid grandchild_pgid=$grandchild_pgid" >&2
    return 1
  fi
  wait_for_dead "$child_pid"
  wait_for_dead "$grandchild_pid"
  if [[ -d "$lock" ]]; then
    echo "$signal 退出后未释放锁" >&2
    return 1
  fi
}

run_signal_case INT 130
run_signal_case TERM 143

run_orphan_signal_case() {
  local signal="$1"
  local want_status="$2"
  local name="orphan_${signal}"
  local trace="$TMP/$name.log"
  local lock="$TMP/$name.lock"
  : >"$trace"
  set +e
  env PATH="$TMP/bin:$PATH" TEST_TRACE="$trace" \
    CORE_TEST_PERSISTENCE_LOCK_DIR="$lock" FAKE_GO_BLOCK=1 FAKE_GO_LEADER_EXIT=1 \
    perl -e '
      my ($trace, $signal, @cmd) = @ARGV;
      my $pid = fork();
      die "fork 失败" unless defined $pid;
      if ($pid == 0) {
        $SIG{INT} = "DEFAULT";
        $SIG{TERM} = "DEFAULT";
        exec @cmd;
        die "exec 失败";
      }
      $SIG{ALRM} = sub {
        my $pgid = "";
        if (open my $fh, "<", $trace) {
          while (<$fh>) { $pgid = $1 if /go-pgid\s+(\d+)/; }
          close $fh;
        }
        kill "KILL", -$pgid if $pgid;
        kill "KILL", $pid;
        waitpid $pid, 0;
        exit 125;
      };
      alarm 5;
      my $ready = 0;
      for (1..200) {
        if (open my $fh, "<", $trace) {
          local $/;
          $ready = 1 if index(<$fh> // "", "go-leader-exit") >= 0;
          close $fh;
        }
        last if $ready;
        select undef, undef, undef, 0.02;
      }
      die "等待 leader 退出超时" unless $ready;
      kill $signal, $pid or die "发送信号失败";
      waitpid $pid, 0;
      alarm 0;
      exit(($? & 127) ? 128 + ($? & 127) : $? >> 8);
    ' "$trace" "$signal" "$ROOT/scripts/test.sh" integration-persistence
  local status=$?
  set -e
  if [[ "$status" -ne "$want_status" ]]; then
    echo "leader 已退出时 $signal 退出码错误: got=$status want=$want_status" >&2
    return 1
  fi
  wait_for_trace "$trace" ' down '
  assert_order "$trace" 'go-leader-exit' ' down '
  if grep -q 'cleanup-while-group-alive' "$trace"; then
    echo "leader 已退出时 $signal cleanup 仍有孙进程存活" >&2
    return 1
  fi
  local grandchild_pid
  grandchild_pid="$(awk '/go-grandchild/ {print $2; exit}' "$trace")"
  wait_for_dead "$grandchild_pid"
  if [[ -d "$lock" ]]; then
    echo "leader 已退出时 $signal 未释放锁" >&2
    return 1
  fi
}

run_orphan_signal_case INT 130
run_orphan_signal_case TERM 143

run_compose_up_signal_case() {
  local signal="$1"
  local want_status="$2"
  local name="compose_up_${signal}"
  local trace="$TMP/$name.log"
  local lock="$TMP/$name.lock"
  : >"$trace"
  set +e
  env PATH="$TMP/bin:$PATH" TEST_TRACE="$trace" \
    CORE_TEST_PERSISTENCE_LOCK_DIR="$lock" FAKE_DOCKER_UP_BLOCK=1 \
    FAKE_DOCKER_UP_GRANDCHILD_IGNORE_SIGNAL="$signal" \
    perl -e '
      my ($trace, $signal, @cmd) = @ARGV;
      my $pid = fork();
      die "fork 失败" unless defined $pid;
      if ($pid == 0) {
        $SIG{INT}="DEFAULT";
        $SIG{TERM}="DEFAULT";
        exec @cmd;
        die "exec 失败";
      }
      $SIG{ALRM} = sub {
        my $pgid = "";
        if (open my $fh, "<", $trace) {
          while (<$fh>) { $pgid = $1 if /docker-up-pgid\s+(\d+)/; }
          close $fh;
        }
        kill "KILL", -$pgid if $pgid;
        kill "KILL", $pid;
        waitpid $pid, 0;
        exit 125;
      };
      alarm 5;
      my $ready=0;
      for (1..200) {
        if (open my $fh, "<", $trace) {
          local $/;
          $ready=1 if index(<$fh> // "", "docker-up-grandchild-pgid") >= 0;
          close $fh;
        }
        last if $ready;
        select undef, undef, undef, 0.02;
      }
      die "等待 compose up 进程组超时" unless $ready;
      kill $signal, $pid or die "发送信号失败";
      waitpid $pid, 0;
      alarm 0;
      exit(($? & 127) ? 128 + ($? & 127) : $? >> 8);
    ' "$trace" "$signal" "$ROOT/scripts/test.sh" integration-persistence
  local status=$?
  set -e
  if [[ "$status" -ne "$want_status" ]]; then
    echo "compose up 阶段 $signal 退出码错误: got=$status want=$want_status" >&2
    return 1
  fi
  wait_for_trace "$trace" "docker-up-signal $signal"
  wait_for_trace "$trace" ' down '
  assert_order "$trace" "docker-up-signal $signal" ' down '
  if [[ "$signal" == "INT" ]]; then
    wait_for_trace "$trace" 'docker-up-grandchild-reaped'
    assert_order "$trace" 'docker-up-grandchild-reaped' ' down '
  fi
  if grep -q 'cleanup-while-compose-up-group-alive' "$trace"; then
    echo "compose up 阶段 $signal cleanup 时进程组仍存活" >&2
    return 1
  fi
  local up_pid up_child
  up_pid="$(awk '/docker-up-start/ {print $2; exit}' "$trace")"
  up_child="$(awk '/docker-up-grandchild / {print $2; exit}' "$trace")"
  wait_for_dead "$up_pid"
  wait_for_dead "$up_child"
  if [[ -d "$lock" ]]; then
    echo "compose up 阶段 $signal 后未释放锁" >&2
    return 1
  fi
}

run_compose_up_signal_case INT 130
run_compose_up_signal_case TERM 143

run_compose_up_timeout_case() {
  local trace="$TMP/compose_up_timeout.log"
  local lock="$TMP/compose_up_timeout.lock"
  : >"$trace"
  set +e
  env PATH="$TMP/bin:$PATH" TEST_TRACE="$trace" \
    CORE_TEST_PERSISTENCE_LOCK_DIR="$lock" CORE_TEST_PERSISTENCE_UP_TIMEOUT_SECONDS=1 \
    FAKE_DOCKER_UP_BLOCK=1 \
    "$ROOT/scripts/test.sh" integration-persistence
  local status=$?
  set -e
  if [[ "$status" -ne 124 ]]; then
    echo "compose up 超时退出码错误: got=$status want=124" >&2
    return 1
  fi
  wait_for_trace "$trace" 'docker-up-signal TERM'
  wait_for_trace "$trace" ' down '
  assert_order "$trace" 'docker-up-signal TERM' ' down '
  if [[ -d "$lock" ]]; then
    echo "compose up 超时后未释放锁" >&2
    return 1
  fi
}

run_compose_up_timeout_case

run_blocked_cleanup_case() {
  local trace="$TMP/blocked_cleanup.log"
  local lock="$TMP/blocked_cleanup.lock"
  : >"$trace"
  set +e
  env PATH="$TMP/bin:$PATH" TEST_TRACE="$trace" \
    CORE_TEST_PERSISTENCE_LOCK_DIR="$lock" CORE_TEST_PERSISTENCE_CLEANUP_TIMEOUT_SECONDS=0.1 \
    FAKE_DOCKER_DOWN_BLOCK=1 \
    perl -e '
      my ($trace, @cmd) = @ARGV;
      my $pid = fork();
      die "fork 失败" unless defined $pid;
      if ($pid == 0) {
        exec @cmd;
        die "exec 失败";
      }
      $SIG{ALRM} = sub {
        my ($down_pid, $down_child) = ("", "");
        if (open my $fh, "<", $trace) {
          while (<$fh>) {
            $down_pid = $1 if /docker-down-start\s+(\d+)/;
            $down_child = $1 if /docker-down-grandchild\s+(\d+)/;
          }
          close $fh;
        }
        kill "KILL", $down_child if $down_child;
        kill "KILL", $down_pid if $down_pid;
        kill "KILL", $pid;
        waitpid $pid, 0;
        exit 125;
      };
      alarm 5;
      waitpid $pid, 0;
      alarm 0;
      exit(($? & 127) ? 128 + ($? & 127) : $? >> 8);
    ' "$trace" "$ROOT/scripts/test.sh" integration-persistence
  local status=$?
  set -e
  if [[ "$status" -eq 0 || "$status" -eq 125 ]]; then
    echo "down 阻塞时应有界失败: got=$status" >&2
    return 1
  fi
  wait_for_trace "$trace" 'docker-down-grandchild-pgid'
  local down_pid down_child
  down_pid="$(awk '/docker-down-start/ {print $2; exit}' "$trace")"
  down_child="$(awk '/docker-down-grandchild / {print $2; exit}' "$trace")"
  wait_for_dead "$down_pid"
  wait_for_dead "$down_child"
  if [[ -d "$lock" ]]; then
    echo "down 阻塞失败后未释放锁" >&2
    return 1
  fi
}

run_blocked_cleanup_case

shared_lock="$TMP/shared.lock"
first_trace="$TMP/lock_first.log"
second_trace="$TMP/lock_second.log"
: >"$first_trace"
: >"$second_trace"
env PATH="$TMP/bin:$PATH" TEST_TRACE="$first_trace" \
  CORE_TEST_PERSISTENCE_LOCK_DIR="$shared_lock" FAKE_GO_BLOCK=1 \
  perl -e '$SIG{INT}="DEFAULT"; $SIG{TERM}="DEFAULT"; exec @ARGV' \
  "$ROOT/scripts/test.sh" integration-persistence &
first_pid=$!
wait_for_trace "$first_trace" 'go-start'

set +e
env PATH="$TMP/bin:$PATH" TEST_TRACE="$second_trace" \
  CORE_TEST_PERSISTENCE_LOCK_DIR="$shared_lock" \
  "$ROOT/scripts/test.sh" integration-persistence
second_status=$?
set -e
if [[ "$second_status" -eq 0 ]]; then
  echo "活跃锁存在时并发任务必须失败" >&2
  exit 1
fi
if grep -q ' up ' "$second_trace"; then
  echo "未取得锁的任务不应启动 compose" >&2
  exit 1
fi
kill -TERM "$first_pid"
set +e
wait "$first_pid"
first_status=$?
set -e
if [[ "$first_status" -ne 143 ]]; then
  echo "锁持有任务退出码错误: got=$first_status want=143" >&2
  exit 1
fi

stale_lock="$TMP/stale.lock"
mkdir "$stale_lock"
echo 99999999 >"$stale_lock/pid"
stale_trace="$TMP/stale.log"
: >"$stale_trace"
set +e
env PATH="$TMP/bin:$PATH" TEST_TRACE="$stale_trace" \
  CORE_TEST_PERSISTENCE_LOCK_DIR="$stale_lock" \
  "$ROOT/scripts/test.sh" integration-persistence
stale_status=$?
set -e
if [[ "$stale_status" -eq 0 ]]; then
  echo "陈旧 PID 锁也必须 fail closed" >&2
  exit 1
fi
if grep -q ' up ' "$stale_trace"; then
  echo "陈旧 PID 锁存在时绝不能启动 compose" >&2
  exit 1
fi
if [[ "$(cat "$stale_lock/pid")" != "99999999" ]]; then
  echo "fail closed 不应修改既有锁 owner" >&2
  exit 1
fi
rm -rf "$stale_lock"

echo "持久化集成脚本生命周期测试通过"
