// 本文件分开SQL、Broker和metrics预算；正式窗口不全表COUNT，终局仍全量核验。
package shoporderscalemultiprocess

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/redis/go-redis/v9"
)

func (s orderLoadSample) converged() bool {
	return len(s.Errors) == 0 && s.Pending != nil && *s.Pending == 0 && s.Unpublished != nil && !*s.Unpublished &&
		s.BrokerPending != nil && *s.BrokerPending == 0 && s.BrokerLag != nil && *s.BrokerLag == 0
}

func takeOrderLoadSample(db *sql.DB, broker *redis.Client, processes []*integration.Suite, full ...bool) orderLoadSample {
	started := time.Now()
	sample := orderLoadSample{At: started.UTC(), Metrics: map[string]float64{}, StageMS: map[string]float64{}}
	var wg sync.WaitGroup
	var mu sync.Mutex
	addError := func(err error) { mu.Lock(); sample.Errors = append(sample.Errors, err.Error()); mu.Unlock() }
	recordTime := func(stage string, start time.Time) {
		mu.Lock()
		sample.StageMS[stage] = time.Since(start).Seconds() * 1000
		mu.Unlock()
	}
	wg.Go(func() {
		start := time.Now()
		defer recordTime("sql", start)
		deadline := 500 * time.Millisecond
		counts := len(full) > 0 && full[0]
		if counts {
			deadline = 3 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), deadline)
		defer cancel()
		if counts {
			var orders, outbox, published int
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM `order`").Scan(&orders); err != nil {
				addError(fmt.Errorf("sql_order_count: %w", err))
				return
			}
			sample.Orders = &orders
			if err := db.QueryRowContext(ctx, "SELECT COUNT(*),COALESCE(SUM(published),0) FROM outbox_record").Scan(&outbox, &published); err != nil {
				addError(fmt.Errorf("sql_outbox_count: %w", err))
				return
			}
			sample.Outbox = &outbox
			sample.Published = &published
			pending := outbox != published
			sample.Unpublished = &pending
		} else {
			var pending bool
			if err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM outbox_record WHERE published = 0 LIMIT 1)").Scan(&pending); err != nil {
				addError(fmt.Errorf("sql_pending_exists: %w", err))
				return
			}
			sample.Unpublished = &pending
		}
	})
	wg.Go(func() {
		start := time.Now()
		defer recordTime("broker", start)
		ctx, cancel := context.WithTimeout(context.Background(), 750*time.Millisecond)
		defer cancel()
		groups, err := broker.XInfoGroups(ctx, "core:event:07:shop.order.changed").Result()
		if err != nil {
			addError(fmt.Errorf("broker: %w", err))
			return
		}
		for _, g := range groups {
			if g.Name == "shop-user" {
				pending, lag, delivered := g.Pending, g.Lag, g.EntriesRead
				sample.BrokerPending = &pending
				sample.BrokerLag = &lag
				sample.BrokerDelivered = &delivered
			}
		}
		if sample.BrokerLag == nil || *sample.BrokerLag < 0 {
			addError(fmt.Errorf("broker: 缺少可确认的消费组lag"))
		}
	})
	for i, p := range processes {
		wg.Go(func() {
			start := time.Now()
			defer recordTime(fmt.Sprintf("metrics_%d", i), start)
			client := http.Client{Timeout: 750 * time.Millisecond}
			resp, err := client.Get("http://127.0.0.1:" + p.Environment["SHOP_METRICS_PORT"] + "/metrics")
			if err != nil {
				addError(fmt.Errorf("metrics_%d: %w", i, err))
				return
			}
			data, readErr := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
			_ = resp.Body.Close()
			if readErr != nil || resp.StatusCode != 200 {
				addError(fmt.Errorf("metrics_%d: status=%d error=%v", i, resp.StatusCode, readErr))
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, line := range strings.Split(string(data), "\n") {
				fields := strings.Fields(line)
				if len(fields) != 2 {
					continue
				}
				value, err := strconv.ParseFloat(fields[1], 64)
				if err != nil {
					continue
				}
				key := fields[0]
				if strings.HasPrefix(key, "native_db_") || strings.HasPrefix(key, "native_phase_") || (strings.HasPrefix(key, "core_component_gauge{") && (strings.Contains(key, `component="outbox"`) || strings.Contains(key, `component="write_path"`))) {
					sample.Metrics[fmt.Sprintf("process_%d_%s", i, key)] = value
				}

				if key == "process_resident_memory_bytes" || key == "process_cpu_seconds_total" || key == "go_goroutines" || key == "go_gc_duration_seconds_sum" || key == "go_gc_duration_seconds_count" || key == "go_memstats_heap_inuse_bytes" || key == "go_memstats_heap_alloc_bytes" {
					sample.Metrics[fmt.Sprintf("process_%d_%s", i, key)] = value
				}
				if i >= 2 && strings.HasPrefix(key, "core_component_gauge{") && strings.Contains(key, `component="pending"`) && strings.Contains(key, `name="depth"`) && strings.Contains(key, `state="ok"`) {
					sample.Pending = &value
				}
				if i >= 2 && strings.HasPrefix(key, "core_component_gauge{") && strings.Contains(key, `component="pending"`) && strings.Contains(key, `name="disk_bytes"`) {
					sample.Metrics["pending_disk_bytes"] = value
				}
			}
		})
	}
	wg.Wait()
	if sample.Pending == nil {
		addError(fmt.Errorf("缺少真实pending depth指标"))
	} else if *sample.Pending < 0 {
		addError(fmt.Errorf("INVALID_STATE: pending负数"))
	}
	sample.StageMS["total"] = time.Since(started).Seconds() * 1000
	sample.At = time.Now().UTC()
	return sample
}
