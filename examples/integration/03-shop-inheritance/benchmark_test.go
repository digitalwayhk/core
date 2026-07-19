package inheritanceshop_test

import (
	"fmt"
	"net/http"
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
)

type benchmarkFixture struct {
	admin       string
	user        string
	product     ProductDTO
	paymentType PaymentTypeDTO
	order       OrderDTO
}

func newBenchmarkFixture(b *testing.B) benchmarkFixture {
	b.Helper()
	suffix := time.Now().UnixNano()
	admin := suite.TokenFor(b, fmt.Sprintf("bench-admin-%d", suffix), 1)
	user := suite.TokenFor(b, fmt.Sprintf("bench-user-%d", suffix), 0)
	product := suite.AddProduct(b, admin, fmt.Sprintf("基准商品-%d", suffix), "12.50")
	paymentType := suite.AddPaymentType(b, admin, fmt.Sprintf("bench-pay-%d", suffix), fmt.Sprintf("基准支付-%d", suffix), true)
	order := suite.AddOrder(b, user, uintID(b, product.ID), 1)
	return benchmarkFixture{admin: admin, user: user, product: product, paymentType: paymentType, order: order}
}

func benchmarkConcurrencies() []int {
	// 正式压力测试可显式指定并发矩阵，例如：
	// SHOP_BENCH_CONCURRENCIES=100,500,1000 go test ... -bench ...
	// 未设置时保留适合日常开发机的 1/GOMAXPROCS/4*GOMAXPROCS 默认值。
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

func runHTTPBenchmark(b *testing.B, operation func(index int) error) {
	for _, concurrency := range benchmarkConcurrencies() {
		concurrency := concurrency
		b.Run(fmt.Sprintf("concurrency-%d", concurrency), func(b *testing.B) {
			const maxSamples = 4096
			samples := make([]int64, 0, min(b.N, maxSamples))
			var sampleMu sync.Mutex
			var next atomic.Int64
			var firstErr error
			var errOnce sync.Once
			start := make(chan struct{})
			var workers sync.WaitGroup
			workers.Add(concurrency)
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
				b.ReportMetric(float64(b.N)/elapsed.Seconds(), "req/s")
			}
			reportLatencyPercentiles(b, samples)
			benchmetrics.Report(b, throughputStats)
		})
	}
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

// shouldSampleLatency 将最多 maxSamples 个样本均匀分布到整个 benchmark 窗口，
// 避免只采集启动阶段请求导致 P95/P99 偏向冷启动。
func shouldSampleLatency(index, total, maxSamples int) bool {
	if total <= maxSamples {
		return true
	}
	step := (total + maxSamples - 1) / maxSamples
	return index%step == 0
}

func requestSucceeded(method, path, token string, body interface{}) error {
	response, err := suite.DoJSON(method, path, token, body)
	if err != nil {
		return err
	}
	if !response.Success {
		return fmt.Errorf("%s %s 失败: %s", method, path, response.ErrorMessage)
	}
	return nil
}

func BenchmarkGetProducts(b *testing.B) {
	fixture := newBenchmarkFixture(b)
	path := "/api/inheritanceshop/getproducts?id=" + fixture.product.ID
	runHTTPBenchmark(b, func(int) error { return requestSucceeded(http.MethodGet, path, "", nil) })
}

func BenchmarkGetSuppliers(b *testing.B) {
	fixture := newBenchmarkFixture(b)
	path := fmt.Sprintf("/api/inheritanceshop/getsuppliers?id=%d", fixture.product.SupplierID)
	runHTTPBenchmark(b, func(int) error { return requestSucceeded(http.MethodGet, path, "", nil) })
}

func BenchmarkGetPaymentTypes(b *testing.B) {
	fixture := newBenchmarkFixture(b)
	path := "/api/inheritanceshop/getpaymenttypes?code=" + fixture.paymentType.Code
	runHTTPBenchmark(b, func(int) error { return requestSucceeded(http.MethodGet, path, "", nil) })
}

func BenchmarkGetOrders(b *testing.B) {
	fixture := newBenchmarkFixture(b)
	runHTTPBenchmark(b, func(int) error {
		return requestSucceeded(http.MethodGet, "/api/inheritanceshop/getorders", fixture.user, nil)
	})
}

func BenchmarkAddOrder(b *testing.B) {
	fixture := newBenchmarkFixture(b)
	for _, concurrency := range benchmarkConcurrencies() {
		concurrency := concurrency
		b.Run(fmt.Sprintf("concurrency-%d", concurrency), func(b *testing.B) {
			tokens := suite.TokenPoolFor(b, fmt.Sprintf("bench-order-%d", time.Now().UnixNano()), 128, 0)
			runHTTPBenchmarkSingleConcurrency(b, concurrency, func(index int) error {
				return requestSucceeded(http.MethodPost, "/api/inheritanceshop/addorder", tokens[index%len(tokens)], map[string]interface{}{
					"productID": uintID(b, fixture.product.ID), "quantity": 1,
				})
			})
		})
	}
}

// BenchmarkMixedWorkload 与示例 4 使用完全相同的 70/20/10 读写比和轮转用户池，
// 避免单用户订单列表持续增长把数据规模成本误计为框架性能退化。
func BenchmarkMixedWorkload(b *testing.B) {
	fixture := newBenchmarkFixture(b)
	productPath := "/api/inheritanceshop/getproducts?id=" + fixture.product.ID
	users := suite.TokenPoolFor(b, fmt.Sprintf("bench-mixed-%d", time.Now().UnixNano()), 128, 0)
	runHTTPBenchmark(b, func(index int) error {
		user := users[benchmetrics.RotatingSlot(index, 10, len(users))]
		switch index % 10 {
		case 0:
			return requestSucceeded(http.MethodPost, "/api/inheritanceshop/addorder", user, map[string]interface{}{
				"productID": uintID(b, fixture.product.ID), "quantity": 1,
			})
		case 1, 2:
			return requestSucceeded(http.MethodGet, "/api/inheritanceshop/getorders", user, nil)
		default:
			return requestSucceeded(http.MethodGet, productPath, "", nil)
		}
	})
}

func runHTTPBenchmarkSingleConcurrency(b *testing.B, concurrency int, operation func(index int) error) {
	const maxSamples = 4096
	samples := make([]int64, 0, min(b.N, maxSamples))
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
	b.ReportMetric(float64(b.N)/elapsed.Seconds(), "orders/s")
	reportLatencyPercentiles(b, samples)
	benchmetrics.Report(b, throughputStats)
}
