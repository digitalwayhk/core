//go:build integration

// 本文件用真实 PostgreSQL 验证自动初始化、CRUD、事务、聚合与断线恢复。
package oltp

import (
	"context"
	"fmt"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"
)

type pgContractRecord struct {
	ID        uint            `gorm:"primaryKey;autoIncrement:false"`
	Hashcode  string          `gorm:"uniqueIndex"`
	Amount    decimal.Decimal `gorm:"type:decimal(38,18)"`
	CreatedAt time.Time
	Enabled   bool
}

// TestPostgresIntegration_ConcurrentProcesses 验证进程间建库及DDL锁，不仅验证本进程互斥。
func TestPostgresIntegration_ConcurrentProcesses(t *testing.T) {
	c := postgresIntegrationConfig(t)
	if os.Getenv("CORE_PG_SCHEMA_CHILD") == "1" {
		require.NoError(t, NewPostgres(c).HasTable(&pgContractRecord{}))
		return
	}
	executable, err := os.Executable()
	require.NoError(t, err)
	database := fmt.Sprintf("core_pg_process_%d", time.Now().UnixNano())
	var wg sync.WaitGroup
	results := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(executable, "-test.run=^TestPostgresIntegration_ConcurrentProcesses$", "-test.timeout=40s")
			cmd.Env = append(os.Environ(), "CORE_PG_SCHEMA_CHILD=1", "CORE_TEST_POSTGRES_DATABASE="+database)
			data, err := cmd.CombinedOutput()
			if err != nil {
				err = fmt.Errorf("%w: %s", err, data)
			}
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
}

func (pgContractRecord) TableName() string { return "pg_contract_records" }

type pgContractView struct{ pgContractRecord }

func (pgContractView) TableName() string { return "pg_contract_view" }
func (pgContractView) SearchSQL() string {
	return "SELECT id, hashcode, amount, created_at, enabled FROM pg_contract_records"
}

type pgDecimalFact struct {
	ID        uint `gorm:"primaryKey"`
	Amount    decimal.Decimal
	CreatedAt time.Time
}

// TestPostgresIntegration_UntaggedDecimal 验证业务模型不加数据库专用tag也能保留金额并聚合。
func TestPostgresIntegration_UntaggedDecimal(t *testing.T) {
	p := NewPostgres(postgresIntegrationConfig(t))
	require.NoError(t, p.HasTable(&pgDecimalFact{}))
	value := decimal.RequireFromString("123456789.123456789123456789")
	require.NoError(t, p.Insert(&pgDecimalFact{Amount: value, CreatedAt: time.Now()}))
	result, err := Aggregate(context.Background(), p, types.AggregateQuery{Fact: &pgDecimalFact{}, Grain: types.GrainDay, Metrics: []types.AggregateMetric{{Kind: types.MetricSum, Field: "Amount"}}})
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, value.String(), result[0].Metrics["sum_amount"])
}

func postgresIntegrationConfig(t testing.TB) types.StorageConfig {
	t.Helper()
	if os.Getenv("CORE_TEST_POSTGRES") != "1" {
		t.Skip("设置 CORE_TEST_POSTGRES=1 后运行真实 PostgreSQL 验收")
	}
	c := types.StorageConfig{Driver: "postgres", Host: "127.0.0.1", Port: 15432, Username: "core_test", Password: "core_pg_test", Database: fmt.Sprintf("core_pg_%d", os.Getpid()), SSLMode: "disable"}
	if value := os.Getenv("CORE_TEST_POSTGRES_PORT"); value != "" {
		port, err := strconv.Atoi(value)
		require.NoError(t, err)
		c.Port = port
	}
	for key, target := range map[string]*string{"HOST": &c.Host, "USER": &c.Username, "PASSWORD": &c.Password, "DATABASE": &c.Database} {
		if value := os.Getenv("CORE_TEST_POSTGRES_" + key); value != "" {
			*target = value
		}
	}
	c.ApplyDefaults()
	return c
}

// TestPostgresIntegration_DriverContract 验证实际存储结果而非仅连接健康。
func TestPostgresIntegration_DriverContract(t *testing.T) {
	p := NewPostgres(postgresIntegrationConfig(t))
	require.NoError(t, p.HasTable(&pgContractRecord{}))
	db, err := p.GetDB()
	require.NoError(t, err)
	require.NoError(t, db.Exec("DELETE FROM pg_contract_records").Error)
	date := time.Date(2021, 1, 1, 12, 0, 0, 0, time.UTC)
	seed := &pgContractRecord{ID: 9007199254740993, Hashcode: "one", Amount: decimal.RequireFromString("123456789.123456789123456789"), CreatedAt: date, Enabled: true}
	require.NoError(t, p.Insert(seed))
	require.Error(t, p.Insert(seed))
	seed.Enabled = false
	seed.Amount = decimal.Zero
	require.NoError(t, p.Update(seed))
	query := &types.SearchItem{Model: &pgContractRecord{}, Page: 1, Size: 10}
	query.AddWhereN("ID", seed.ID)
	var rows []*pgContractRecord
	require.NoError(t, p.Load(query, &rows))
	require.Len(t, rows, 1)
	require.False(t, rows[0].Enabled)
	require.True(t, rows[0].Amount.IsZero())
	tx := p.Clone()
	require.NoError(t, tx.Transaction())
	require.NoError(t, tx.Insert(&pgContractRecord{ID: 2, Hashcode: "rollback", CreatedAt: date}))
	require.NoError(t, tx.Rollback())
	query = &types.SearchItem{Model: &pgContractRecord{}, Page: 1, Size: 10}
	query.AddWhereN("ID", 2)
	rows = nil
	require.NoError(t, p.Load(query, &rows))
	require.Empty(t, rows)
	result, err := Aggregate(context.Background(), p, types.AggregateQuery{Fact: &pgContractRecord{}, Grain: types.GrainWeek, Metrics: []types.AggregateMetric{{Kind: types.MetricCount}}})
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, "2020-W53", result[0].Bucket)
	require.Equal(t, "1", result[0].Metrics["row_count"])
	// 标准 IDBSQL 路径仍绑定查询参数，不能依赖反引号替换。
	view := &types.SearchItem{Model: &pgContractView{}, Page: 1, Size: 5}
	view.AddWhereN("Hashcode", "one")
	var views []*pgContractView
	require.NoError(t, p.Load(view, &views))
	require.Len(t, views, 1)
	pool, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, pool.Close())
	require.NoError(t, p.Load(view, &views))
	require.Len(t, views, 1)
	require.NoError(t, p.Delete(seed))
}

// TestPostgresIntegration_ConcurrentSchema 验证多个独立 action 并发初始化同一库表。
func TestPostgresIntegration_ConcurrentSchema(t *testing.T) {
	c := postgresIntegrationConfig(t)
	c.Database += "_concurrent"
	var wg sync.WaitGroup
	failures := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); failures <- NewPostgres(c).HasTable(&pgContractRecord{}) }()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
}

// TestPostgresIntegration_ConflictPreservesTransaction 验证唯一冲突后可重查且不丢失事务内已有写入。
func TestPostgresIntegration_ConflictPreservesTransaction(t *testing.T) {
	c := postgresIntegrationConfig(t)
	c.Database += "_conflict"
	p := NewPostgres(c)
	require.NoError(t, p.HasTable(&pgContractRecord{}))
	require.NoError(t, p.Insert(&pgContractRecord{ID: 1, Hashcode: "existing", CreatedAt: time.Now()}))
	tx := p.Clone()
	require.NoError(t, tx.Transaction())
	require.NoError(t, tx.Insert(&pgContractRecord{ID: 2, Hashcode: "before", CreatedAt: time.Now()}))
	require.Error(t, tx.Insert(&pgContractRecord{ID: 3, Hashcode: "existing", CreatedAt: time.Now()}))
	var rows []*pgContractRecord
	require.NoError(t, tx.Load(&types.SearchItem{Model: &pgContractRecord{}, Page: 1, Size: 10}, &rows))
	require.Len(t, rows, 2)
	require.NoError(t, tx.Commit())
	rows = nil
	require.NoError(t, p.Load(&types.SearchItem{Model: &pgContractRecord{}, Page: 1, Size: 10}, &rows))
	require.Len(t, rows, 2)
}
