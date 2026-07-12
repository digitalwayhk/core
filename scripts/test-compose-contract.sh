#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
compose="$ROOT/docker-compose.integration.yml"
env_example="$ROOT/.env.integration.example"

for service in etcd consul redis nats kafka mysql mongodb clickhouse; do
  grep -Eq "^  ${service}:" "$compose" || { echo "Compose 缺少服务: $service" >&2; exit 1; }
done
grep -A2 '^  kafka:' "$compose" | grep -Fq 'profiles: ["kafka"]'
for service in mysql mongodb clickhouse; do
  grep -A3 "^  ${service}:" "$compose" | grep -Fq 'profiles: ["persistence"]'
done
if grep -Eq 'image:[[:space:]]+[^#[:space:]]+:(latest|main|master)[[:space:]]*$' "$compose"; then
  echo "Compose 禁止浮动镜像 tag" >&2
  exit 1
fi
if grep -Eq '"0\.0\.0\.0:[0-9]+:' "$compose"; then
  echo "Compose 端口必须绑定回环地址" >&2
  exit 1
fi
for variable in CORE_TEST_ETCD CORE_TEST_CONSUL CORE_TEST_REDIS_STREAM CORE_TEST_NATS; do
  grep -Eq "^${variable}=1$" "$env_example" || { echo "环境样例缺少 $variable" >&2; exit 1; }
done
grep -Fq 'Core 未内建 Kafka MQProvider' "$env_example"

echo "Docker Compose 静态契约测试通过"
