//go:build corebench

// 本文件仅在显式压测诊断时采集运行时与阻塞栈，不进入默认示例二进制。
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
)

func init() {
	dir := os.Getenv("CORE_BENCH_DIAG_DIR")
	if dir == "" {
		return
	}
	go collectBenchmarkRuntime(dir)
}

func collectBenchmarkRuntime(dir string) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return
	}
	output, err := os.Create(filepath.Join(dir, "order-runtime.jsonl"))
	if err != nil {
		return
	}
	defer output.Close()
	encoder := json.NewEncoder(output)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var previousWait int64
	var lastStack time.Time
	profileStarted := false
	for now := range ticker.C {
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		values := map[string]interface{}{"at": now.UTC(), "gomaxprocs": runtime.GOMAXPROCS(0), "goroutines": runtime.NumGoroutine(), "heap_alloc": memory.HeapAlloc, "heap_sys": memory.HeapSys, "total_alloc": memory.TotalAlloc, "num_gc": memory.NumGC, "gc_pause_total_ns": memory.PauseTotalNs, "gc_cpu_fraction": memory.GCCPUFraction}
		for key, connection := range oltp.GetConnectionManager().GetAllConnections() {
			if !strings.HasPrefix(key, "mysql:") || connection == nil || connection.DB == nil {
				continue
			}
			db, err := connection.DB.DB()
			if err != nil {
				continue
			}
			stats := db.Stats()
			values["pool"] = stats
			if stats.WaitCount-previousWait >= 5 && now.Sub(lastStack) >= time.Second {
				file, err := os.Create(filepath.Join(dir, fmt.Sprintf("order-goroutines-%d.txt", now.UnixMilli())))
				if err == nil {
					_ = pprof.Lookup("goroutine").WriteTo(file, 2)
					_ = file.Close()
				}
				lastStack = now
			}
			previousWait = stats.WaitCount
			if !profileStarted && stats.OpenConnections > 0 && os.Getenv("CORE_BENCH_DIAG_PROFILE") == "1" {
				profileStarted = true
				go func() {
					file, err := os.Create(filepath.Join(dir, "order-cpu.pprof"))
					if err != nil {
						return
					}
					defer file.Close()
					if pprof.StartCPUProfile(file) == nil {
						time.Sleep(20 * time.Second)
						pprof.StopCPUProfile()
					}
				}()
			}
		}
		_ = encoder.Encode(values)
	}
}
