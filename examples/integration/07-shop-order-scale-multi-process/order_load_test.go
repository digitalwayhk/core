// 本文件以显式开关运行真实三进程订单负载，区分目标速率、确认吞吐和后台排空。
package shoporderscalemultiprocess

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	orderdto "github.com/digitalwayhk/core/examples/07-shop-order-scale/dto/order"
	integration "github.com/digitalwayhk/core/examples/integration"
	"github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type orderLoadResult struct {
	RequestID  string  `json:"request_id"`
	OrderID    uint    `json:"order_id,omitempty"`
	Status     int     `json:"status"`
	Success    bool    `json:"success"`
	Error      string  `json:"error,omitempty"`
	StartedMS  float64 `json:"started_ms"`
	DurationMS float64 `json:"duration_ms"`
}

type orderLoadSample struct {
	BrokerPending   *int64             `json:"broker_pending"`
	BrokerLag       *int64             `json:"broker_lag"`
	BrokerDelivered *int64             `json:"broker_delivered"`
	At              time.Time          `json:"at"`
	Pending         *float64           `json:"pending"`
	Orders          int                `json:"orders"`
	Outbox          int                `json:"outbox"`
	Published       int                `json:"published"`
	Metrics         map[string]float64 `json:"metrics"`
	Errors          []string           `json:"errors,omitempty"`
}

// TestStableOrderWriteLoad 默认跳过；只接受显式提供的隔离依赖，不修改业务调度。
func TestStableOrderWriteLoad(t *testing.T) {
	if os.Getenv("CORE_TEST_ORDER_LOAD") != "1" {
		t.Skip("仅显式 CORE_TEST_ORDER_LOAD=1 运行压测")
	}
	loadSeconds, err := parseOrderLoadSeconds(os.Getenv("CORE_TEST_LOAD_SECONDS"))
	require.NoError(t, err)
	evidence := os.Getenv("CORE_TEST_LOAD_EVIDENCE_DIR")
	require.NotEmpty(t, evidence, "需要明确证据目录")
	require.True(t, filepath.IsAbs(evidence))
	require.NoError(t, os.MkdirAll(evidence, 0700))
	_, ns, processes := startStableNativeProcesses(t, 1)
	t.Cleanup(func() {
		for i, p := range processes {
			data, err := os.ReadFile(filepath.Join(p.RootDir, "service.log"))
			if err == nil {
				require.NoError(t, os.WriteFile(filepath.Join(evidence, fmt.Sprintf("process-%d.log", i)), data, 0600))
			}
		}
	})
	admin := prepareDockerAdminFixture(t, processes[1])
	supplier := prepareDockerSupplierFixture(t, processes[1], admin)
	user := processes[0]
	token := user.TokenFor(t, "720001", 0)
	warmup := createDockerBuyerOrderWithRequest(t, user, token, supplier.ProductID, "load-warmup")
	waitDockerOrderVisible(t, user, token, warmup.OrderID)
	config := mysql.NewConfig()
	config.User, config.Passwd, config.Net, config.Addr, config.DBName = "root", os.Getenv("CORE_TEST_STABLE_MYSQL_PASSWORD"), "tcp", os.Getenv("CORE_TEST_STABLE_MYSQL_ADDR"), "core_"+ns
	db, err := sql.Open("mysql", config.FormatDSN())
	require.NoError(t, err)
	defer db.Close()
	db.SetMaxOpenConns(3)
	broker := redis.NewClient(&redis.Options{Addr: os.Getenv("CORE_TEST_REDIS_ADDR")})
	defer broker.Close()
	sampleCtx, stopSample := context.WithCancel(context.Background())
	loadCtx, stopLoad := context.WithCancel(context.Background())
	defer stopLoad()
	guardianStop := ""
	var samples []orderLoadSample
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		consecutiveErrors := 0
		for {
			sample := takeOrderLoadSample(db, broker, processes)
			samples = append(samples, sample)
			if len(sample.Errors) > 0 {
				consecutiveErrors++
			} else {
				consecutiveErrors = 0
			}
			if guardianStop == "" {
				if sample.Pending != nil && *sample.Pending >= 40_000 {
					guardianStop = "pending 达到 40000，停止发送，保留恢复数据"
				}
				if consecutiveErrors >= 3 {
					guardianStop = "连续三次观测失败，停止发送"
				}
				if guardianStop != "" {
					stopLoad()
				}
			}
			select {
			case <-sampleCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	defer func() { stopSample(); <-done }()
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{MaxIdleConns: 512, MaxIdleConnsPerHost: 512, MaxConnsPerHost: 512}}
	defer client.CloseIdleConnections()
	start := time.Now()
	var results []orderLoadResult
	var stages []map[string]interface{}
	for _, stage := range []struct{ rate, seconds int }{{500, 10}, {2000, loadSeconds}} {
		if loadCtx.Err() != nil {
			break
		}
		batch, summary := runOrderLoadStage(loadCtx, client, user.BaseURL, token, supplier.ProductID, stage.rate, time.Duration(stage.seconds)*time.Second, start)
		results = append(results, batch...)
		stages = append(stages, summary)
		t.Logf("负载窗口：%s", mustLoadJSON(summary))
	}
	loadEnd := time.Now()
	drained := false
	storeDrainSeconds := -1.0
	for deadline := time.Now().Add(360 * time.Second); time.Now().Before(deadline); {
		sample := takeOrderLoadSample(db, broker, processes)
		if len(sample.Errors) == 0 && sample.Pending != nil && *sample.Pending == 0 && sample.Orders == sample.Outbox && sample.Outbox == sample.Published {
			if storeDrainSeconds < 0 {
				storeDrainSeconds = time.Since(loadEnd).Seconds()
			}
			if sample.BrokerPending != nil && sample.BrokerLag != nil && *sample.BrokerPending == 0 && *sample.BrokerLag == 0 {
				drained = true
				break
			}
		}
		time.Sleep(time.Second)
	}
	stopSample()
	<-done
	// defer 中重复读取已关闭 channel 安全；采样结果在 join 后读取。
	final := takeOrderLoadSample(db, broker, processes)
	writeLoadJSON(t, filepath.Join(evidence, "requests.json"), results)
	writeLoadJSON(t, filepath.Join(evidence, "samples.json"), samples)
	output, _ := exec.Command("git", "rev-parse", "HEAD").Output()
	summary := map[string]interface{}{"source_head": strings.TrimSpace(string(output)), "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "logical_cpus": runtime.NumCPU(), "order_replicas": 1, "namespace": ns, "stages": stages, "drained": drained, "drain_seconds": time.Since(loadEnd).Seconds(), "store_drain_seconds": storeDrainSeconds, "final": final, "sample_count": len(samples)}
	// 核对每个已确认的 requestID/OrderID；超时失败可能已落库，单独登记未知结果，不能当作已确认丢失。
	rows, err := db.Query("SELECT id,request_id FROM `order`")
	require.NoError(t, err)
	persisted := make(map[string]uint)
	for rows.Next() {
		var id uint
		var request string
		require.NoError(t, rows.Scan(&id, &request))
		persisted[request] = id
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	confirmed, missing, unexpected := 0, 0, 0
	for _, result := range results {
		if result.Success {
			confirmed++
			if persisted[result.RequestID] != result.OrderID {
				missing++
			}
		} else if persisted[result.RequestID] != 0 {
			unexpected++
		}
	}
	summary["confirmed"], summary["confirmed_missing_or_mismatched"], summary["failed_request_but_persisted"] = confirmed, missing, unexpected
	sampleErrors := 0
	for _, sample := range samples {
		sampleErrors += len(sample.Errors)
	}
	summary["sample_errors"] = sampleErrors
	summary["requested_load_seconds"], summary["guardian_stop"] = loadSeconds, guardianStop
	writeLoadJSON(t, filepath.Join(evidence, "summary.json"), summary)
	t.Logf("负载结果：%s", mustLoadJSON(summary))
	require.True(t, drained, "停止发送后 360 秒持久化或消费 ACK 仍未排空")
	require.Zero(t, missing, "已确认订单不得丢失或改变主键")
	require.Zero(t, sampleErrors, "观测缺失必须标记为不完整，不能用零代替")
	require.Empty(t, guardianStop, "守护停发必须判定未达标，不能把提前结束当容量通过")
	for _, stage := range stages {
		require.Equal(t, false, stage["interrupted"])
		require.Zero(t, stage["generator_dropped"], "发送器丢弃不能冒充服务吞吐")
		require.Equal(t, stage["scheduled"], stage["started"])
		require.GreaterOrEqual(t, float64(stage["successful"].(int))/float64(stage["scheduled"].(int)), 0.999)
	}
}

func parseOrderLoadSeconds(value string) (int, error) {
	if value == "" {
		return 30, nil
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 1 || seconds > 600 {
		return 0, fmt.Errorf("CORE_TEST_LOAD_SECONDS 必须为 1 至 600")
	}
	return seconds, nil
}

func runOrderLoadStage(ctx context.Context, client *http.Client, url, token string, product uint, rate int, duration time.Duration, origin time.Time) ([]orderLoadResult, map[string]interface{}) {
	start := time.Now()
	total := int(duration.Seconds() * float64(rate))
	slots := make(chan struct{}, 512)
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make([]orderLoadResult, 0, total)
	dropped, maxLagMS := 0, 0.0
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
sendLoop:
	for scheduled := 0; scheduled < total; {
		var now time.Time
		select {
		case <-ctx.Done():
			break sendLoop
		case now = <-ticker.C:
		}
		due := int(now.Sub(start).Seconds() * float64(rate))
		if due > total {
			due = total
		}
		for scheduled < due {
			if ctx.Err() != nil {
				break sendLoop
			}
			index := scheduled
			scheduled++
			lag := time.Since(start).Seconds()*1000 - float64(index)*1000/float64(rate)
			if lag > maxLagMS {
				maxLagMS = lag
			}
			select {
			case slots <- struct{}{}:
			default:
				dropped++
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-slots }()
				requestID := fmt.Sprintf("load-%d-%d", rate, index)
				body := fmt.Sprintf(`{"productID":%d,"quantity":2,"requestID":%q,"address":{"addressID":1,"receiverName":"压测买家","phone":"13800000000","province":"广东","city":"深圳","district":"南山","detail":"测试"}}`, product, requestID)
				began := time.Now()
				result := orderLoadResult{RequestID: requestID, StartedMS: began.Sub(origin).Seconds() * 1000}
				req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url+"/api/shop-user/addorder", bytes.NewBufferString(body))
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")
				resp, err := client.Do(req)
				if err != nil {
					result.Error = err.Error()
				} else {
					result.Status = resp.StatusCode
					var envelope integration.ResponseEnvelope
					data, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
					_ = resp.Body.Close()
					if readErr != nil {
						result.Error = readErr.Error()
					} else if err = json.Unmarshal(data, &envelope); err != nil {
						result.Error = "响应不是 JSON"
					} else if !envelope.Success {
						result.Error = envelope.ErrorMessage
					} else {
						var order orderdto.Order
						if err = json.Unmarshal(envelope.Data, &order); err != nil || order.OrderID == 0 {
							result.Error = "响应缺少合法 orderID"
						} else {
							result.Success = true
							result.OrderID = order.OrderID
						}
					}
				}
				result.DurationMS = time.Since(began).Seconds() * 1000
				mu.Lock()
				results = append(results, result)
				mu.Unlock()
			}()
		}
	}
	wg.Wait()
	elapsed := time.Since(start).Seconds()
	success := 0
	latency := make([]float64, 0, len(results))
	statuses := make(map[string]int)
	for _, result := range results {
		if result.Success {
			success++
		}
		latency = append(latency, result.DurationMS)
		key := strconv.Itoa(result.Status) + ":" + result.Error
		statuses[key]++
	}
	sort.Float64s(latency)
	quantile := func(p float64) float64 {
		if len(latency) == 0 {
			return 0
		}
		return latency[int(float64(len(latency)-1)*p)]
	}
	return results, map[string]interface{}{"target_rps": rate, "window_seconds": duration.Seconds(), "elapsed_including_inflight_seconds": elapsed, "scheduled": total, "started": len(results), "generator_dropped": dropped, "generator_max_lag_ms": maxLagMS, "successful": success, "failed": len(results) - success, "success_rps_over_window": float64(success) / duration.Seconds(), "success_rps_including_inflight": float64(success) / elapsed, "p50_ms": quantile(.50), "p95_ms": quantile(.95), "p99_ms": quantile(.99), "statuses": statuses, "window_start_ms": start.Sub(origin).Seconds() * 1000, "interrupted": ctx.Err() != nil}
}

func takeOrderLoadSample(db *sql.DB, broker *redis.Client, processes []*integration.Suite) orderLoadSample {
	sample := orderLoadSample{At: time.Now().UTC(), Metrics: map[string]float64{}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM `order`").Scan(&sample.Orders); err != nil {
		sample.Errors = append(sample.Errors, err.Error())
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*),COALESCE(SUM(published),0) FROM outbox_record").Scan(&sample.Outbox, &sample.Published); err != nil {
		sample.Errors = append(sample.Errors, err.Error())
	}
	groups, groupErr := broker.XInfoGroups(ctx, "core:event:07:shop.order.changed").Result()
	if groupErr != nil {
		sample.Errors = append(sample.Errors, groupErr.Error())
	} else {
		for _, group := range groups {
			if group.Name == "shop-user" {
				pending, lag, delivered := group.Pending, group.Lag, group.EntriesRead
				sample.BrokerPending = &pending
				sample.BrokerLag = &lag
				sample.BrokerDelivered = &delivered
			}
		}
		if sample.BrokerLag == nil || *sample.BrokerLag < 0 {
			sample.Errors = append(sample.Errors, "缺少可确认的消费组 lag")
		}
	}
	client := http.Client{Timeout: 2 * time.Second}
	for i, p := range processes {
		resp, err := client.Get("http://127.0.0.1:" + p.Environment["SHOP_METRICS_PORT"] + "/metrics")
		if err != nil {
			sample.Errors = append(sample.Errors, err.Error())
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		_ = resp.Body.Close()
		if err != nil || resp.StatusCode != 200 {
			sample.Errors = append(sample.Errors, fmt.Sprintf("metrics process=%d status=%d error=%v", i, resp.StatusCode, err))
			continue
		}
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
			if key == "process_resident_memory_bytes" || key == "process_cpu_seconds_total" || key == "go_goroutines" {
				sample.Metrics[fmt.Sprintf("process_%d_%s", i, key)] = value
			}
			if i >= 2 && strings.HasPrefix(key, "core_component_gauge{") && strings.Contains(key, `component="pending"`) && strings.Contains(key, `name="depth"`) && strings.Contains(key, `state="ok"`) {
				sample.Pending = &value
			}
			if i >= 2 && strings.HasPrefix(key, "core_component_gauge{") && strings.Contains(key, `component="pending"`) && strings.Contains(key, `name="disk_bytes"`) {
				sample.Metrics["pending_disk_bytes"] = value
			}
		}
	}
	if sample.Pending == nil {
		sample.Errors = append(sample.Errors, "缺少真实 pending depth 指标")
	}
	return sample
}

func writeLoadJSON(t *testing.T, path string, value interface{}) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(data, '\n'), 0600))
}
func mustLoadJSON(value interface{}) string { data, _ := json.Marshal(value); return string(data) }
