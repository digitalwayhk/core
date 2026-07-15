#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "$0")/.." && pwd)
ARTIFACT_DIR=${BENCHMARK_ARTIFACT_DIR:-"/tmp/shop-benchmark-$(date +%Y%m%d-%H%M%S)"}
BENCHTIME=${SHOP_BENCHTIME:-1s}
mkdir -p "$ARTIFACT_DIR"

run_benchmark() {
  local package=$1
  local output=$2
  (
    cd "$ROOT_DIR"
    GOCACHE=${GOCACHE:-/private/tmp/core-codex-go-cache} go test "$package" \
      -run '^$' -bench 'Benchmark(Get|Add|Mixed)' -benchmem -benchtime "$BENCHTIME" -count=1 -timeout=30m
  ) | tee "$output"
}

extract_metrics() {
  awk '
    /^Benchmark/ {
      name=$1
      sub(/-[0-9]+$/, "", name)
      throughput=""
      unit=""
      for (i=2; i<=NF; i++) {
        if ($i == "req/s" || $i == "orders/s") {
          throughput=$(i-1)
          unit=$i
        }
      }
      if (throughput != "") print name "\t" throughput "\t" unit
    }
  ' "$1" | sort -u
}

BASELINE_OUTPUT="$ARTIFACT_DIR/03-shop-inheritance.txt"
OPTIMIZED_OUTPUT="$ARTIFACT_DIR/04-shop-performance.txt"
run_benchmark ./examples/integration/03-shop-inheritance "$BASELINE_OUTPUT"
run_benchmark ./examples/integration/04-shop-performance "$OPTIMIZED_OUTPUT"

extract_metrics "$BASELINE_OUTPUT" > "$ARTIFACT_DIR/03.tsv"
extract_metrics "$OPTIMIZED_OUTPUT" > "$ARTIFACT_DIR/04.tsv"
join -t $'\t' "$ARTIFACT_DIR/03.tsv" "$ARTIFACT_DIR/04.tsv" > "$ARTIFACT_DIR/joined.tsv"

REPORT="$ARTIFACT_DIR/商城示例性能对比.md"
{
  echo "# 商城示例性能对比"
  echo
  echo "- 基线：示例 3 SQLite 直读直写"
  echo "- 优化：示例 4 RouterInfo L1/L2 缓存与 Badger 写后同步"
  echo "- benchtime：$BENCHTIME"
  echo "- 说明：结果仅用于同机同次对比，不作为 CI 固定倍数门禁。"
  echo
  echo '| Benchmark | 示例 3 | 示例 4 | 单位 | 变化 |'
  echo '| --- | ---: | ---: | --- | ---: |'
  awk -F '\t' '{
    ratio = ($2 == 0 ? 0 : (($4 / $2) - 1) * 100)
    printf "| %s | %.2f | %.2f | %s | %+.2f%% |\n", $1, $2, $4, $3, ratio
  }' "$ARTIFACT_DIR/joined.tsv"
  echo
  echo "原始结果："
  echo
  echo "- \`$BASELINE_OUTPUT\`"
  echo "- \`$OPTIMIZED_OUTPUT\`"
} > "$REPORT"

printf '性能对比报告：%s\n' "$REPORT"
