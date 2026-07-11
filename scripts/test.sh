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
      ./pkg/server/types \
      ./pkg/server/cluster \
      -count=1
    go test -race \
      ./pkg/server/run \
      ./pkg/server/router \
      ./pkg/server/cluster \
      ./pkg/server/trans/rest \
      -run 'Test.*Lifecycle|Test.*Concurrent.*Start.*Stop|Test.*Shutdown' \
      -count=20
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
    echo "usage: scripts/test.sh {quick|server|security|concurrency|integration-local|integration-external|all}" >&2
    exit 2
    ;;
esac
