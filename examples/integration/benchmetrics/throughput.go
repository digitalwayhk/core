package benchmetrics

import (
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// MinimumDistributionWindows 是输出吞吐分位数所需的最少完整窗口数。
// 默认一秒窗口下对应至少 30 秒，避免把烟测的一两个样本误读为稳定性分布。
const MinimumDistributionWindows = 30

// Stats 描述单次 benchmark 中“每秒吞吐”样本的分布，不是请求延迟分位数。
type Stats struct {
	Windows      int
	P01          float64
	P05          float64
	P50          float64
	P95          float64
	P99          float64
	Mean         float64
	Min          float64
	Max          float64
	StdDev       float64
	CVPercent    float64
	Errors       uint64
	ErrorPercent float64
}

// Collector 以一秒为默认窗口低成本采集吞吐，请求热路径只做原子计数。
type Collector struct {
	window    time.Duration
	startedAt time.Time
	successes atomic.Uint64
	errors    atomic.Uint64
	stop      chan struct{}
	done      chan struct{}
	once      sync.Once
	mu        sync.Mutex
	samples   []float64
}

func NewCollector(window time.Duration) *Collector {
	if window <= 0 {
		window = time.Second
	}
	collector := &Collector{
		window:    window,
		startedAt: time.Now(),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	go collector.run()
	return collector
}

func (c *Collector) RecordSuccess() { c.successes.Add(1) }
func (c *Collector) RecordError()   { c.errors.Add(1) }

func (c *Collector) Stop() Stats {
	if c == nil {
		return Stats{}
	}
	c.once.Do(func() { close(c.stop) })
	<-c.done
	c.mu.Lock()
	samples := append([]float64(nil), c.samples...)
	c.mu.Unlock()
	return Summarize(samples, c.successes.Load(), c.errors.Load())
}

func (c *Collector) run() {
	ticker := time.NewTicker(c.window)
	defer ticker.Stop()
	defer close(c.done)
	lastAt := c.startedAt
	lastCount := uint64(0)
	for {
		select {
		case now := <-ticker.C:
			lastCount = c.appendWindow(lastAt, now, lastCount, false)
			lastAt = now
		case <-c.stop:
			now := time.Now()
			c.appendWindow(lastAt, now, lastCount, true)
			return
		}
	}
}

func (c *Collector) appendWindow(from, to time.Time, previous uint64, partial bool) uint64 {
	current := c.successes.Load()
	duration := to.Sub(from)
	// 尾部不完整窗口至少覆盖半个窗口才纳入，避免几毫秒样本放大噪声。
	if duration > 0 && current > previous && (!partial || duration >= c.window/2) {
		c.mu.Lock()
		c.samples = append(c.samples, float64(current-previous)/duration.Seconds())
		c.mu.Unlock()
	}
	return current
}

// Summarize 计算吞吐窗口分布，单元测试可直接传入确定样本。
func Summarize(samples []float64, successes, errors uint64) Stats {
	result := Stats{Windows: len(samples), Errors: errors}
	if total := successes + errors; total > 0 {
		result.ErrorPercent = float64(errors) * 100 / float64(total)
	}
	if len(samples) == 0 {
		return result
	}
	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)
	result.Min = sorted[0]
	result.Max = sorted[len(sorted)-1]
	result.P01 = percentile(sorted, 1)
	result.P05 = percentile(sorted, 5)
	result.P50 = percentile(sorted, 50)
	result.P95 = percentile(sorted, 95)
	result.P99 = percentile(sorted, 99)
	for _, sample := range sorted {
		result.Mean += sample
	}
	result.Mean /= float64(len(sorted))
	for _, sample := range sorted {
		delta := sample - result.Mean
		result.StdDev += delta * delta
	}
	result.StdDev = math.Sqrt(result.StdDev / float64(len(sorted)))
	if result.Mean > 0 {
		result.CVPercent = result.StdDev * 100 / result.Mean
	}
	return result
}

func percentile(sorted []float64, percent int) float64 {
	index := (len(sorted) - 1) * percent / 100
	return sorted[index]
}

// Reporter 与 testing.B 的 ReportMetric 兼容，避免通用包依赖 testing。
type Reporter interface {
	ReportMetric(float64, string)
}

func Report(reporter Reporter, stats Stats) {
	reporter.ReportMetric(float64(stats.Windows), "win-windows")
	if stats.Windows >= MinimumDistributionWindows {
		reporter.ReportMetric(stats.P01, "win-p01/s")
		reporter.ReportMetric(stats.P05, "win-p05/s")
		reporter.ReportMetric(stats.P50, "win-p50/s")
		reporter.ReportMetric(stats.P95, "win-p95/s")
		reporter.ReportMetric(stats.P99, "win-p99/s")
		reporter.ReportMetric(stats.Mean, "win-mean/s")
		reporter.ReportMetric(stats.StdDev, "win-stddev/s")
		reporter.ReportMetric(stats.CVPercent, "win-cv-pct")
	}
	reporter.ReportMetric(float64(stats.Errors), "errors")
	reporter.ReportMetric(stats.ErrorPercent, "error-pct")
}

// RotatingSlot 把一组连续操作稳定映射到轮转槽位，用于长稳基准分散单用户数据增长。
func RotatingSlot(operationIndex, operationsPerSlot, slotCount int) int {
	if operationIndex < 0 || operationsPerSlot <= 0 || slotCount <= 0 {
		return 0
	}
	return (operationIndex / operationsPerSlot) % slotCount
}
