//go:build corebench

// 本文件仅在corebench压测构建中采集真实SQL连接池，不进入默认示例二进制。
package main

import (
	"database/sql"
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/prometheus/client_golang/prometheus"
)

type benchmarkPoolCollector struct{}

var benchmarkPoolMetrics = []string{"known", "open", "in_use", "idle", "max_open", "wait_count_total", "wait_seconds_total"}

func benchmarkPoolDesc(name string) *prometheus.Desc {
	return prometheus.NewDesc("core_benchmark_sql_pool_"+name, "压测构建的实际SQL连接池统计", []string{"driver"}, nil)
}

// Describe 声明仅在压测构建中存在的连接池指标。
func (benchmarkPoolCollector) Describe(output chan<- *prometheus.Desc) {
	for _, name := range benchmarkPoolMetrics {
		output <- benchmarkPoolDesc(name)
	}
}

// Collect 从应用本进程真实连接池读取统计，不能用压测观察者自己的池代替。
func (benchmarkPoolCollector) Collect(output chan<- prometheus.Metric) {
	for _, driver := range []string{"mysql", "postgres"} {
		var stats sql.DBStats
		known := 0.0
		for _, connection := range oltp.GetConnectionManager().GetAllConnections() {
			if connection == nil || connection.DB == nil || connection.DB.Dialector.Name() != driver {
				continue
			}
			db, err := connection.DB.DB()
			if err != nil {
				continue
			}
			known = 1
			s := db.Stats()
			stats.OpenConnections += s.OpenConnections
			stats.InUse += s.InUse
			stats.Idle += s.Idle
			stats.MaxOpenConnections += s.MaxOpenConnections
			stats.WaitCount += s.WaitCount
			stats.WaitDuration += s.WaitDuration
		}
		values := []float64{known, float64(stats.OpenConnections), float64(stats.InUse), float64(stats.Idle), float64(stats.MaxOpenConnections), float64(stats.WaitCount), stats.WaitDuration.Seconds()}
		for i, name := range benchmarkPoolMetrics {
			kind := prometheus.GaugeValue
			if strings.HasSuffix(name, "_total") {
				kind = prometheus.CounterValue
			}
			output <- prometheus.MustNewConstMetric(benchmarkPoolDesc(name), kind, values[i], driver)
		}
	}
}

func init() { prometheus.MustRegister(benchmarkPoolCollector{}) }
