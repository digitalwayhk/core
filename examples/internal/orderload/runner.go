// 本文件复用07原发送算法；仅使用标准库，不加载Core或go-zero。
package orderload

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

// RunStage 按固定速率发送一阶段，限制512在途、1MiB响应且不自动重试。
func RunStage(ctx context.Context, client *http.Client, url, token string, product uint, rate int, duration time.Duration, origin time.Time, namespace string) ([]Result, map[string]interface{}) {
	start := time.Now()
	total := int(duration.Seconds() * float64(rate))
	slots := make(chan struct{}, 512)
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make([]Result, 0, total)
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
				requestID := fmt.Sprintf("load-%s-%d-%d", namespace, rate, index)
				body := fmt.Sprintf(`{"productID":%d,"quantity":2,"requestID":%q,"address":{"addressID":1,"receiverName":"压测买家","phone":"13800000000","province":"广东","city":"深圳","district":"南山","detail":"测试"}}`, product, requestID)
				began := time.Now()
				result := Result{RequestID: requestID, StartedMS: began.Sub(origin).Seconds() * 1000}
				req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url+"/api/shop-user/addorder", bytes.NewBufferString(body))
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")
				resp, err := client.Do(req)
				if err != nil {
					result.Error = err.Error()
				} else {
					result.Status = resp.StatusCode
					var envelope responseEnvelope
					data, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
					_ = resp.Body.Close()
					if readErr != nil {
						result.Error = readErr.Error()
					} else if err = json.Unmarshal(data, &envelope); err != nil {
						result.Error = "响应不是 JSON"
					} else if !envelope.Success {
						result.Error = envelope.ErrorMessage
					} else {
						var order struct {
							OrderID uint `json:"orderID"`
						}
						if err = json.Unmarshal(envelope.Data, &order); err != nil || order.OrderID == 0 {
							result.Error = "响应缺少合法 orderID"
						} else {
							result.Success = true
							result.OrderID = order.OrderID
						}
					}
				}
				if !result.Success {
					if result.Status == 0 {
						var timeout net.Error
						if errors.Is(err, context.Canceled) {
							result.FailureKind = "cancelled"
						} else if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
							result.FailureKind = "timeout"
						} else {
							result.FailureKind = "transport"
						}
					} else if result.Status >= 500 {
						result.FailureKind = "http_server"
					} else if result.Status == 401 || result.Status == 403 {
						result.FailureKind = "authentication"
					} else if result.Error == "响应不是 JSON" || result.Error == "响应缺少合法 orderID" {
						result.FailureKind = "invalid_response"
					} else {
						result.FailureKind = "application"
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
	inflightEnd := time.Now()
	elapsed := inflightEnd.Sub(start).Seconds()
	success := 0
	latency := make([]float64, 0, len(results))
	statuses := make(map[string]int)
	failureClasses := make(map[string]int)
	for _, result := range results {
		if result.Success {
			success++
		}
		if result.FailureKind != "" {
			failureClasses[result.FailureKind]++
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
	return results, map[string]interface{}{"target_rps": rate, "window_seconds": duration.Seconds(), "elapsed_including_inflight_seconds": elapsed, "inflight_end_at": inflightEnd.UTC().Format(time.RFC3339Nano), "scheduled": total, "started": len(results), "generator_dropped": dropped, "generator_not_started": total - len(results) - dropped, "generator_max_lag_ms": maxLagMS, "successful": success, "failed": len(results) - success, "success_rps_over_window": float64(success) / duration.Seconds(), "success_rps_including_inflight": float64(success) / elapsed, "p50_ms": quantile(.50), "p95_ms": quantile(.95), "p99_ms": quantile(.99), "statuses": statuses, "failure_classes": failureClasses, "window_start_ms": start.Sub(origin).Seconds() * 1000, "interrupted": ctx.Err() != nil}
}
