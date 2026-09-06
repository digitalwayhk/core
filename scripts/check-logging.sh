#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

failed=0

check_forbidden() {
  local description="$1"
  local pattern="$2"
  local matches
  matches="$(rg -n "$pattern" pkg service --glob '*.go' --glob '!**/*_test.go' || true)"
  if [[ -n "$matches" ]]; then
    printf '%s\n' "$matches"
    printf '禁止的运行时日志: %s\n' "$description" >&2
    failed=1
  fi
}

check_forbidden "标准控制台或进程终止 logger" '^[[:space:]]*(fmt\.(Print|Printf|Println)|log\.(Print|Printf|Println|Fatal|Fatalf|Panic|Panicf))'
check_forbidden "装饰性日志输出" '^[[:space:]]*logx\..*[🚀✅⚠️❌🛑📊🆕🔗║╚🔄🔧⏳♻️🗑️🔍]'
check_forbidden "敏感值日志表达式" '^[[:space:]]*(logx\.|fmt\.|log\.).*(token|password|passwd|secret|authorization|cookie|totp)'
check_forbidden "完整 payload、response、SQL 或参数日志" '^[[:space:]]*logx\..*(PrintObj\(|string\(values\)|Statement\.SQL|查询 SQL|查询参数|sql:%s)'

if rg -n -U 'mq_redis_reliable_(keyed_)?handler_failed",[\s\S]{0,200}logx\.Field\("error"' \
  pkg/server/mq/provider_redis.go >/dev/null; then
  printf '%s\n' "禁止 MQ handler 原始错误进入日志，避免业务 payload 或消息身份随 error 泄露" >&2
  failed=1
fi

exit "$failed"
