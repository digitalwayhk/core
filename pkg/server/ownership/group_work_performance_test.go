// 本文件以固定SQL事实和连接预算比较旧Step整批与显式组模式；只在独占真实依赖时显式运行。
package ownership_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/digitalwayhk/core/pkg/utils"
	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGroupWorkSQLPerformanceComparison(t *testing.T) {
	if os.Getenv("CORE_TEST_GROUP_WORK_PERFORMANCE") != "1" {
		t.Skip("NOT_RUN: explicit real SQL performance window")
	}
	seconds := 2
	if value := os.Getenv("CORE_TEST_GROUP_WORK_PERFORMANCE_SECONDS"); value != "" {
		parsed, err := strconv.Atoi(value)
		require.NoError(t, err)
		require.GreaterOrEqual(t, parsed, 1)
		require.LessOrEqual(t, parsed, 60)
		seconds = parsed
	}
	batches := 25 * seconds
	expected := 8 * batches
	oldP := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(oldP)
	for round := 0; round < 3; round++ {
		for _, mode := range []string{"legacy_exclusive", "legacy_shared", "group_shared"} {
			t.Run(fmt.Sprintf("%d_%s", round, mode), func(t *testing.T) {
				db := mysqlFenceDatabase(t, &ownershipSQLTask{}).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
				sqlDB, err := db.DB()
				require.NoError(t, err)
				sqlDB.SetMaxOpenConns(8)
				sqlDB.SetMaxIdleConns(8)
				store, m := groupWorkFixture(t, ownership.Timing{})
				ctx := context.Background()
				b := budgetForTest(t, 3, 6)
				require.NoError(t, db.Create(&mysqlFenceFixture{Scope: m.Scope, KeyName: "bucket"}).Error)
				var mu sync.Mutex
				created := map[uint]time.Time{}
				latencies := []float64{}
				var calls, errorsCount, lastSQLReturn atomic.Int64
				var errorMu sync.Mutex
				errorClasses := map[string]int{}
				next := func(ctx context.Context, c ownership.Credential, q ownership.GroupWorkRequest) ([]string, error) {
					return sqlReadyGroups(ctx, db, c, q)
				}
				step := func(ctx context.Context, c ownership.Credential, g string) (bool, error) {
					calls.Add(1)
					var row ownershipSQLTask
					progress := false
					err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
						var err error
						if mode == "legacy_exclusive" {
							err = ownership.CheckFence(mysqlFenceTransaction{tx}, c)
						} else {
							err = ownership.CheckFenceShared(mysqlSharedFenceTransaction{tx}, c)
						}
						if err != nil {
							return err
						}
						if err = tx.Where("owner_key = ? AND order_key = ? AND state = ?", c.Key, g, "pending").Order("ordinal").Take(&row).Error; err != nil {
							if err == gorm.ErrRecordNotFound {
								return nil
							}
							return err
						}
						delay := 4 * time.Millisecond
						if g == "group-0" {
							delay = 24 * time.Millisecond
						}
						timer := time.NewTimer(delay)
						defer timer.Stop()
						select {
						case <-timer.C:
						case <-ctx.Done():
							return ctx.Err()
						}
						r := tx.Model(&ownershipSQLTask{}).Where("id = ? AND state = ?", row.ID, "pending").Updates(map[string]any{"state": "done", "writes": gorm.Expr("writes+1")})
						progress = r.RowsAffected == 1
						return r.Error
					})
					if err != nil {
						errorsCount.Add(1)
						class := "other"
						var sqlErr *mysqldriver.MySQLError
						if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
							class = "canceled"
						} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
							class = "deadline"
						} else if errors.As(err, &sqlErr) {
							class = fmt.Sprintf("mysql_%d", sqlErr.Number)
						}
						errorMu.Lock()
						errorClasses[class]++
						errorMu.Unlock()
					}
					if progress && err == nil {
						committedAt := time.Now()
						for previous := lastSQLReturn.Load(); committedAt.UnixNano() > previous && !lastSQLReturn.CompareAndSwap(previous, committedAt.UnixNano()); previous = lastSQLReturn.Load() {
						}
						mu.Lock()
						latencies = append(latencies, float64(committedAt.Sub(created[row.ID]))/float64(time.Millisecond))
						mu.Unlock()
					}
					return progress, err
				}
				opts := ownership.Options{Manifest: m, InstanceID: "owner", SessionID: mode, MaxOwned: 1, Activate: func(ctx context.Context, c ownership.Credential) error {
					return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return ownership.ActivateFence(mysqlFenceTransaction{tx}, c) })
				}, Observe: func(ctx context.Context, c ownership.Credential) (ownership.Observation, error) {
					return taskObservation(ctx, db, c.Key)
				}}
				if mode == "group_shared" {
					opts.GroupWork = &ownership.GroupWorkOptions{Budget: b, NextGroups: next, StepGroup: step}
				} else {
					opts.Step = func(ctx context.Context, c ownership.Credential) (bool, error) {
						groups, err := next(ctx, c, ownership.GroupWorkRequest{Limit: 6})
						if err != nil {
							return false, err
						}
						var progressed atomic.Bool
						tasks := utils.ConcurrencyTasks[string]{Params: groups, Concurrency: 3, Func: func(g string) (interface{}, error) {
							p, e := step(ctx, c, g)
							if p && e == nil {
								progressed.Store(true)
							}
							return nil, e
						}}
						tasks.Run()
						return progressed.Load(), tasks.GetErr()
					}
				}
				rt, err := ownership.StartRuntime(ctx, store, opts)
				require.NoError(t, err)
				defer rt.Close(ctx)
				start := time.Now()
				peakPending := int64(0)
				// 三轮、每轮原SQL持续输入显式秒数（默认2s）；相同8组/40ms、同payload和实际提交，准备阶段不计入窗口。
				for n := 1; n <= batches; n++ {
					tick := start.Add(time.Duration(n-1) * 40 * time.Millisecond)
					if d := time.Until(tick); d > 0 {
						time.Sleep(d)
					}
					rows := []ownershipSQLTask{}
					for g := 0; g < 8; g++ {
						rows = append(rows, ownershipSQLTask{OwnerKey: "bucket", OrderKey: fmt.Sprintf("group-%d", g), Ordinal: n, State: "pending"})
					}
					mu.Lock()
					sent := time.Now()
					require.NoError(t, db.Create(&rows).Error)
					for _, row := range rows {
						created[row.ID] = sent
					}
					mu.Unlock()
					var pending int64
					require.NoError(t, db.Model(&ownershipSQLTask{}).Where("state = ?", "pending").Count(&pending).Error)
					if pending > peakPending {
						peakPending = pending
					}
				}
				inputEnd := time.Now()
				require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(latencies) == expected }, time.Duration(10*seconds)*time.Second, 5*time.Millisecond)

				require.Eventually(t, func() bool {
					if mode == "group_shared" {
						return b.RuntimeMetricSnapshot(ctx).Counters["ownership_work_completed_total"] == float64(expected)
					}
					status, err := rt.Status(ctx, "bucket")
					return err == nil && status.LastConfirmed != nil && status.LastConfirmed.UnixNano() >= lastSQLReturn.Load()
				}, time.Second, time.Millisecond, "SQL返回后仍需等Core实际确认，不能用Close提前结束计时")
				end := time.Now()
				runningErrors := errorsCount.Load()
				require.NoError(t, rt.Close(ctx))
				mu.Lock()
				values := append([]float64(nil), latencies...)
				mu.Unlock()
				sort.Float64s(values)
				var sum float64
				for _, v := range values {
					sum += v
				}
				q := func(p float64) float64 { return values[int(float64(len(values)-1)*p)] }
				var writes int64
				require.NoError(t, db.Model(&ownershipSQLTask{}).Select("SUM(writes)").Scan(&writes).Error)
				require.EqualValues(t, expected, writes)
				stats := sqlDB.Stats()
				out := map[string]any{"mode": mode, "round": round, "timing": m.Timing, "running_errors": runningErrors, "close_errors": errorsCount.Load() - runningErrors, "error_classes": errorClasses, "gomaxprocs": 2, "sql_max_open": 8, "worker_budget": 3, "prefetch_budget": 6, "legacy_batch_limit": 6, "completed": len(values), "writes": writes, "errors": errorsCount.Load(), "calls": calls.Load(), "input_seconds": inputEnd.Sub(start).Seconds(), "total_seconds": end.Sub(start).Seconds(), "drain_seconds": end.Sub(inputEnd).Seconds(), "confirmed_per_second": float64(expected) / end.Sub(start).Seconds(), "admit_to_sql_commit_ms": map[string]float64{"avg": sum / float64(len(values)), "p50": q(.5), "p95": q(.95), "p99": q(.99), "max": values[len(values)-1]}, "peak_pending": peakPending, "sql_pool_wait_count": stats.WaitCount, "sql_pool_wait_seconds": stats.WaitDuration.Seconds(), "component": b.RuntimeMetricSnapshot(ctx), "save_latency": nil, "cpu_throttling": nil, "io": nil}
				raw, err := json.Marshal(out)
				require.NoError(t, err)
				t.Log(string(raw))
				require.Zero(t, runningErrors, "运行期错误不能靠Close分类隐藏")
			})
		}
	}
}
