// 本文件以显式开关运行真实三进程订单负载，区分目标速率、确认吞吐和后台排空。
package shoporderscalemultiprocess

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/internal/orderload"
	"github.com/go-sql-driver/mysql"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type orderLoadResult = orderload.Result

type orderLoadSample struct {
	BrokerPending   *int64             `json:"broker_pending"`
	BrokerLag       *int64             `json:"broker_lag"`
	BrokerDelivered *int64             `json:"broker_delivered"`
	At              time.Time          `json:"at"`
	Pending         *float64           `json:"pending"`
	Orders          *int               `json:"orders"`
	Outbox          *int               `json:"outbox"`
	Published       *int               `json:"published"`
	Metrics         map[string]float64 `json:"metrics"`
	Unpublished     *bool              `json:"unpublished"`
	StageMS         map[string]float64 `json:"stage_ms"`
	Errors          []string           `json:"errors,omitempty"`
}

// TestStableOrderWriteLoad 默认跳过；只接受显式提供的隔离依赖，不修改业务调度。
func TestStableOrderWriteLoad(t *testing.T) {
	runOrderWriteLoad(t, false)
}

// TestNativeGoZeroOrderWriteLoad 仅替换被测应用，实现与 Core 相同的负载和最终逐键核对。
func TestNativeGoZeroOrderWriteLoad(t *testing.T) {
	runOrderWriteLoad(t, true)
}

func runOrderWriteLoad(t *testing.T, native bool) {
	if os.Getenv("CORE_TEST_ORDER_LOAD") != "1" {
		t.Skip("仅显式 CORE_TEST_ORDER_LOAD=1 运行压测")
	}
	loadRate, rateErr := parseOrderLoadRate(os.Getenv("CORE_TEST_LOAD_RATE"))
	require.NoError(t, rateErr)
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
	implementation := "core"
	if native {
		processes = replaceWithNativeGoZero(t, processes, evidence)
		user = processes[0]
		implementation = "go-zero-native"
	}
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
	initial := takeOrderLoadSample(db, broker, processes, true)
	require.Empty(t, initial.Errors, "起始观测不完整")
	sampleCtx, stopSample := context.WithCancel(context.Background())
	loadCtx, stopLoad := context.WithCancel(context.Background())
	defer stopLoad()
	guardianStop := ""
	var samples []orderLoadSample
	var samplesMu sync.Mutex
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		consecutiveErrors := 0
		for {
			sample := takeOrderLoadSample(db, broker, processes)
			samplesMu.Lock()
			samples = append(samples, sample)
			samplesMu.Unlock()
			if len(sample.Errors) > 0 {
				consecutiveErrors++
			} else {
				consecutiveErrors = 0
			}
			if guardianStop == "" {
				if sample.Pending != nil && *sample.Pending < 0 {
					guardianStop = "INVALID_STATE: pending负数，停止发送并保留现场"
				}
				if sample.Pending != nil && *sample.Pending >= 40_000 {
					guardianStop = "pending 达到 40000，停止发送，保留恢复数据"
				}
				if consecutiveErrors >= 3 {
					guardianStop = "连续三次观测失败，停止发送"
				}
				if guardianStop != "" {
					stopLoad()
					captureOrderLoadDiagnostics(t, evidence, processes, guardianStop)
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
	senderBinary := filepath.Join(evidence, "order-load")
	build := exec.Command("go", "build", "-o", senderBinary, "github.com/digitalwayhk/core/examples/07-shop-order-scale/tools/order-load")
	buildOutput, buildErr := build.CombinedOutput()
	require.NoError(t, buildErr, "构建独立发送器: %s", buildOutput)
	runNamespace := fmt.Sprintf("%x", time.Now().UnixNano())
	start := time.Now()
	var results []orderLoadResult
	var stages []map[string]interface{}
	for index, stage := range []struct{ rate, seconds int }{{500, 10}, {loadRate, loadSeconds}} {
		if loadCtx.Err() != nil {
			break
		}
		waitProfiles := func() {}
		if index == 1 && os.Getenv("CORE_TEST_LOAD_PROFILE") == "1" {
			waitProfiles = startOrderProfiles(t, evidence, processes)
		}
		batch, summary := runExternalOrderLoadStage(t, loadCtx, senderBinary, orderload.Stage{
			URL: user.BaseURL, Token: token, ProductID: supplier.ProductID, Rate: stage.rate, DurationMS: int64(stage.seconds) * 1000,
			Origin: start, Namespace: fmt.Sprintf("%s-%d", runNamespace, index), Output: filepath.Join(evidence, fmt.Sprintf("stage-%d", index)),
		})
		waitProfiles()
		results = append(results, batch...)
		stages = append(stages, summary)
		t.Logf("负载窗口：%s", mustLoadJSON(summary))
	}
	loadEnd := time.Now()
	if len(stages) > 0 {
		ended, ok := stages[len(stages)-1]["inflight_end_at"].(string)
		require.True(t, ok)
		loadEnd, err = time.Parse(time.RFC3339Nano, ended)
		require.NoError(t, err)
	}
	drained := false
	storeDrainSeconds := -1.0
	drainSeconds := -1.0
	for {
		samplesMu.Lock()
		storeDrainSeconds, drainSeconds = observedOrderDrain(loadEnd, samples)
		samplesMu.Unlock()
		if drainSeconds >= 0 {
			drained = true
			break
		}
		if !time.Now().Before(loadEnd.Add(360 * time.Second)) {
			break
		}
		time.Sleep(time.Second)
	}
	stopSample()
	<-done
	// defer 中重复读取已关闭 channel 安全；采样结果在 join 后读取。
	verificationStart := time.Now()
	final := takeOrderLoadSample(db, broker, processes, true)
	if !final.converged() || final.Orders == nil || final.Outbox == nil || final.Published == nil || *final.Orders != *final.Outbox || *final.Outbox != *final.Published {
		drained = false
	}
	writeLoadJSON(t, filepath.Join(evidence, "requests.json"), results)
	writeLoadJSON(t, filepath.Join(evidence, "samples.json"), samples)
	output, _ := exec.Command("git", "rev-parse", "HEAD").Output()
	summary := map[string]interface{}{"source_head": strings.TrimSpace(string(output)), "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "logical_cpus": runtime.NumCPU(), "order_replicas": 1, "namespace": ns, "stages": stages, "drained": drained, "drain_seconds": drainSeconds, "initial": initial, "drain_probe_interval_ms": 1000, "store_drain_seconds": storeDrainSeconds, "final": final, "sample_count": len(samples)}
	summary["implementation"] = implementation
	summary["drain_timing_source"] = "completed_background_samples"
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
	if missing != 0 {
		drained = false
		summary["drained"] = false
	}
	sampleErrors := 0
	for _, sample := range samples {
		sampleErrors += len(sample.Errors)
	}
	summary["sample_errors"] = sampleErrors
	summary["full_verification_seconds"] = time.Since(verificationStart).Seconds()
	summary["requested_load_seconds"], summary["guardian_stop"] = loadSeconds, guardianStop
	writeLoadJSON(t, filepath.Join(evidence, "summary.json"), summary)
	t.Logf("负载结果：%s", mustLoadJSON(summary))
	require.True(t, drained, "停止发送后 360 秒持久化或消费 ACK 仍未排空")
	require.LessOrEqual(t, drainSeconds, 5.0, "排空超过5秒，保留证据并判定目标未达标")
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

func parseOrderLoadRate(value string) (int, error) {
	if value == "" {
		return 2000, nil
	}
	rate, err := strconv.Atoi(value)
	if err != nil || (rate != 500 && rate != 1000 && rate != 1500 && rate != 2000) {
		return 0, fmt.Errorf("CORE_TEST_LOAD_RATE必须为500、1000、1500或2000")
	}
	return rate, nil
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
	return orderload.RunStage(ctx, client, url, token, product, rate, duration, origin, fmt.Sprintf("unit-%x", time.Now().UnixNano()))
}

func writeLoadJSON(t *testing.T, path string, value interface{}) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(data, '\n'), 0600))
}
func mustLoadJSON(value interface{}) string { data, _ := json.Marshal(value); return string(data) }
