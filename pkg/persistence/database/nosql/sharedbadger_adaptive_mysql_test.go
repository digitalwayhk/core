// 本文件用显式隔离 MySQL 比较三种调度策略；缺少专用 DSN 时明确跳过。
package nosql

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	_ "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

type measuredLedgerTarget struct {
	inner   WriteBehindTarget[testLedger]
	mu      sync.Mutex
	sizes   []int
	latency []time.Duration
}

func (m *measuredLedgerTarget) SyncBatch(ctx context.Context, items []*SyncQueueItem[testLedger]) (*WriteBehindResult, error) {
	result, err := m.inner.SyncBatch(ctx, items)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sizes = append(m.sizes, len(items))
	if err == nil {
		for _, item := range items {
			m.latency = append(m.latency, time.Since(item.UpdatedAt))
		}
	}
	return result, err
}

func mysqlAdaptiveCounters(t *testing.T, db *sql.DB) map[string]uint64 {
	t.Helper()
	rows, err := db.Query("SHOW GLOBAL STATUS WHERE Variable_name IN ('Com_commit','Innodb_os_log_written','Innodb_data_fsyncs','Innodb_os_log_fsyncs')")
	require.NoError(t, err)
	defer rows.Close()
	values := map[string]uint64{}
	for rows.Next() {
		var key, value string
		require.NoError(t, rows.Scan(&key, &value))
		v, err := strconv.ParseUint(value, 10, 64)
		require.NoError(t, err)
		values[key] = v
	}
	require.NoError(t, rows.Err())
	return values
}

// TestAdaptiveMySQLComparison 验证真实事务、远端行数和 pending 收敛；不代表 Bitzoom R60 UAT。
func TestAdaptiveMySQLComparison(t *testing.T) {
	addr := os.Getenv("CORE_ADAPTIVE_MYSQL_ADDR")
	if addr == "" {
		t.Skip("NOT RUN: 需要专用 CORE_ADAPTIVE_MYSQL_ADDR，禁止指向应用数据库")
	}
	host, portText, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)
	observer, err := sql.Open("mysql", "root@tcp("+addr+")/?parseTime=true")
	require.NoError(t, err)
	defer observer.Close()
	require.NoError(t, observer.Ping())
	for _, load := range []string{"low", "burst", "backlog"} {
		for _, mode := range []string{"legacy100", "fixed10", "adaptive"} {
			t.Run(load+"/"+mode, func(t *testing.T) {
				dbName := fmt.Sprintf("core_adaptive_%d", time.Now().UnixNano())
				action := oltp.NewMySQL(&oltp.Config{Host: host, Port: port, Username: "root"})
				inner := NewModelListWriteBehindTarget(entity.NewModelList[testLedger](action))
				// 首次 schema 创建和连接预热在计量窗口外，仍通过框架自动完成。
				_, err := inner.SyncBatch(context.Background(), []*SyncQueueItem[testLedger]{{Key: "warm", Op: OpInsert, Item: newLedger("warm", "warm", 1, dbName)}})
				require.NoError(t, err)
				path := t.TempDir()
				cfg := DefaultProductionConfig(path)
				cfg.SyncBatchSize = 512
				cfg.SyncBatchDelay = 100 * time.Millisecond
				if mode == "fixed10" {
					cfg.SyncBatchDelay = 10 * time.Millisecond
				}
				if mode == "adaptive" {
					cfg.SyncFlushThreshold = 32
					cfg.SyncMaxCollectDelay = 20 * time.Millisecond
				}
				db, err := NewSharedBadgerDB[testLedger](path, cfg)
				require.NoError(t, err)
				target := &measuredLedgerTarget{inner: inner}
				require.NoError(t, db.UseWriteBehind(target))
				defer func() { _ = db.Close(); _ = CloseSharedManager(path) }()
				n := 256
				if load == "low" {
					n = 8
				}
				if load == "backlog" {
					n = 1536
				}
				items := make([]*testLedger, n)
				for i := range items {
					items[i] = newLedger("load", fmt.Sprint(i), float64(i), dbName)
				}
				if load == "backlog" {
					require.NoError(t, db.BatchInsert(items))
				}
				before := mysqlAdaptiveCounters(t, observer)
				started := time.Now()
				db.manager.config.AutoSync = true // 仅在启动 worker 前赋值
				db.startWriteBehindWorker()
				if load != "backlog" {
					// 旧 worker 完成空索引初始化后，再开始受控输入，避免启动恢复路径混入。
					time.Sleep(20 * time.Millisecond)
					if load == "burst" {
						require.NoError(t, db.BatchInsert(items))
					} else {
						for _, item := range items {
							require.NoError(t, db.Set(item, 0))
							time.Sleep(40 * time.Millisecond)
						}
					}
				}
				require.Eventually(t, func() bool { return db.GetCachedPendingSyncCount() == 0 }, 30*time.Second, time.Millisecond)
				elapsed := time.Since(started)
				after := mysqlAdaptiveCounters(t, observer)
				delta := map[string]uint64{}
				for key, value := range after {
					if previous, ok := before[key]; ok {
						delta[key] = value - previous
					}
				}
				require.Zero(t, db.GetSyncMetrics().Failures)
				// 隔离库只由框架建立一个模型表，直接核验持久化结果（包括一条预热记录）。
				tableRows, err := observer.Query("SHOW TABLES FROM `" + dbName + "`")
				require.NoError(t, err)
				require.True(t, tableRows.Next())
				var table string
				require.NoError(t, tableRows.Scan(&table))
				require.NoError(t, tableRows.Close())
				var persisted int
				require.NoError(t, observer.QueryRow("SELECT COUNT(*) FROM `"+dbName+"`.`"+table+"`").Scan(&persisted))
				require.Equal(t, n+1, persisted)
				target.mu.Lock()
				sizes := append([]int(nil), target.sizes...)
				latencies := append([]time.Duration(nil), target.latency...)
				target.mu.Unlock()
				require.Len(t, latencies, n)
				sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
				t.Logf("RESULT load=%s mode=%s n=%d elapsed=%s batches=%v p50=%s p95=%s p99=%s counters=%v pending=0", load, mode, n, elapsed, sizes, latencies[n/2], latencies[(n-1)*95/100], latencies[(n-1)*99/100], delta)
			})
		}
	}
}
