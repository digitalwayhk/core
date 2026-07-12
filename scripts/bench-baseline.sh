#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

benchtime="${CORE_BENCH_TIME:-200ms}"
count="${CORE_BENCH_COUNT:-3}"

go test ./pkg/server/router ./pkg/server/cluster ./pkg/server/event ./pkg/server/types \
  -run '^$' \
  -bench 'Benchmark(ServiceContextRegistryLookup|LocalProviderListRunningNodes|StreamPublishTenSubscribers|WebSocketNotificationQueueSubmit)$' \
  -benchmem -benchtime="$benchtime" -count="$count"

go test ./pkg/persistence/database/nosql \
  -run '^$' -bench 'Benchmark(Set_Sequential|Get_Sequential)$' \
  -benchmem -benchtime="$benchtime" -count="$count"

go test ./pkg/persistence/database/test \
  -run '^$' -bench 'BenchmarkSqlite_(Insert|Query)$' \
  -benchmem -benchtime="$benchtime" -count="$count"
