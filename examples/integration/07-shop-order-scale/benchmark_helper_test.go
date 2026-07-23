// Package shoporderscale 提供 07 benchmark 与 04 保持一致的并发和指标辅助能力。
package shoporderscale

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/integration/benchmetrics"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/yitter/idgenerator-go/idgen"
)

type benchmarkIDFactory struct {
	worker idgen.ISnowWorker
}

func newBenchmarkIDFactory(machineID uint) benchmarkIDFactory {
	return benchmarkIDFactory{worker: utils.NewAlgorithmSnowFlake(machineID, 4)}
}

func (f benchmarkIDFactory) NewID() uint {
	return uint(f.worker.NextId())
}

func benchmarkConcurrencies() []int {
	if configured := strings.TrimSpace(os.Getenv("SHOP_BENCH_CONCURRENCIES")); configured != "" {
		parts := strings.Split(configured, ",")
		result := make([]int, 0, len(parts))
		seen := make(map[int]struct{}, len(parts))
		for _, part := range parts {
			value, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil || value <= 0 {
				panic(fmt.Sprintf("SHOP_BENCH_CONCURRENCIES 包含无效并发值 %q", part))
			}
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			result = append(result, value)
		}
		return result
	}
	values := []int{1, runtime.GOMAXPROCS(0), 4 * runtime.GOMAXPROCS(0)}
	result := make([]int, 0, len(values))
	seen := make(map[int]struct{})
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// benchmarkReplicaCounts 返回水平扩容基准需要创建的独立 order-service 副本数。
func benchmarkReplicaCounts() []int {
	if configured := strings.TrimSpace(os.Getenv("SHOP_BENCH_REPLICAS")); configured != "" {
		parts := strings.Split(configured, ",")
		result := make([]int, 0, len(parts))
		seen := make(map[int]struct{}, len(parts))
		for _, part := range parts {
			value, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil || value <= 0 {
				panic(fmt.Sprintf("SHOP_BENCH_REPLICAS 包含无效副本数 %q", part))
			}
			if _, exists := seen[value]; exists {
				continue
			}
			seen[value] = struct{}{}
			result = append(result, value)
		}
		return result
	}
	return []int{1, 2, 4}
}

func runBusinessBenchmarkSingleConcurrency(b *testing.B, concurrency int, metricName string, operation func(index int) error) time.Duration {
	const maxSamples = 4096
	samples := make([]int64, 0, benchMin(b.N, maxSamples))
	var sampleMu sync.Mutex
	var next atomic.Int64
	var firstErr error
	var errOnce sync.Once
	var workers sync.WaitGroup
	workers.Add(concurrency)
	start := make(chan struct{})
	b.ReportAllocs()
	b.ResetTimer()
	startedAt := time.Now()
	throughput := benchmetrics.NewCollector(time.Second)
	for worker := 0; worker < concurrency; worker++ {
		go func() {
			defer workers.Done()
			<-start
			for {
				index := int(next.Add(1) - 1)
				if index >= b.N {
					return
				}
				requestStartedAt := time.Now()
				if err := operation(index); err != nil {
					throughput.RecordError()
					errOnce.Do(func() { firstErr = err })
					return
				}
				throughput.RecordSuccess()
				if shouldSampleLatency(index, b.N, maxSamples) {
					sampleMu.Lock()
					samples = append(samples, time.Since(requestStartedAt).Nanoseconds())
					sampleMu.Unlock()
				}
			}
		}()
	}
	close(start)
	workers.Wait()
	elapsed := time.Since(startedAt)
	throughputStats := throughput.Stop()
	b.StopTimer()
	if firstErr != nil {
		b.Fatal(firstErr)
	}
	if elapsed > 0 {
		b.ReportMetric(float64(b.N)/elapsed.Seconds(), metricName)
	}
	reportLatencyPercentiles(b, samples)
	benchmetrics.Report(b, throughputStats)
	return elapsed
}

func reportLatencyPercentiles(b *testing.B, samples []int64) {
	if len(samples) == 0 {
		return
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	b.ReportMetric(float64(samples[(len(samples)-1)*50/100]), "p50-ns")
	b.ReportMetric(float64(samples[(len(samples)-1)*95/100]), "p95-ns")
	b.ReportMetric(float64(samples[(len(samples)-1)*99/100]), "p99-ns")
}

func shouldSampleLatency(index, total, maxSamples int) bool {
	if total <= maxSamples {
		return true
	}
	step := (total + maxSamples - 1) / maxSamples
	return index%step == 0
}

func benchMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
