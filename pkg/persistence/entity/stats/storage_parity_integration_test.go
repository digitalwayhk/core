//go:build integration

// 本文件用同一 StatSpec 验证 SQLite/MySQL/PostgreSQL 的统计语义一致。
package stats

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"os"
	"strconv"
	"testing"
	"time"
)

type storageParityFact struct {
	ID        uint `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time
	Amount    decimal.Decimal `gorm:"type:decimal(38,18)"`
	Label     string          `gorm:"column:order"`
	Enabled   bool
	Optional  sql.NullString
}

func (storageParityFact) TableName() string { return "storage_parity_fact" }

// TestStorageParityIntegration 验证同一定义和结果，不把健康检查当作统计验收。
func TestStorageParityIntegration(t *testing.T) {
	for _, driver := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			c := types.StorageConfig{Driver: driver, Database: fmt.Sprintf("core_parity_%d", time.Now().UnixNano()), Host: "127.0.0.1", Username: "core_test", SSLMode: "disable"}
			if driver != "sqlite" {
				prefix := "CORE_TEST_MYSQL"
				port := 13306
				c.Password = "core_test_password"
				if driver == "postgres" {
					prefix = "CORE_TEST_POSTGRES"
					port = 15432
					c.Password = "core_pg_test"
				}
				if os.Getenv(prefix) != "1" {
					t.Skip("需要显式启用对应的真实数据库")
				}
				if v := os.Getenv(prefix + "_PORT"); v != "" {
					parsed, err := strconv.Atoi(v)
					require.NoError(t, err)
					port = parsed
				}
				c.Port = port
				for suffix, target := range map[string]*string{"HOST": &c.Host, "USER": &c.Username, "PASSWORD": &c.Password, "DATABASE": &c.Database} {
					if v := os.Getenv(prefix + "_" + suffix); v != "" {
						*target = v
					}
				}
			}
			action, err := oltp.NewDataAction(c)
			require.NoError(t, err)
			require.NoError(t, action.(types.IDataBase).HasTable(&storageParityFact{}))
			handle, err := action.GetModelDB(&storageParityFact{})
			require.NoError(t, err)
			require.NoError(t, handle.(*gorm.DB).Exec("DELETE FROM storage_parity_fact").Error)
			day := time.Date(2021, 1, 1, 10, 0, 0, 0, time.UTC)
			for i, amount := range []string{"10.25", "21"} {
				require.NoError(t, action.Insert(&storageParityFact{ID: uint(i + 1), CreatedAt: day, Amount: decimal.RequireFromString(amount), Label: "shared", Enabled: true}))
			}
			query := &types.SearchItem{Model: &storageParityFact{}, Page: 1, Size: 1}
			query.AddWhereN("Label", "shared")
			query.AddWhereN("Enabled", true)
			query.AddWhereNS("ID", types.SymbolIn, []uint{1, 2})
			query.AddWhereNS("Optional", types.SymbolIsNull, nil)
			query.AddWhereNS("ID", types.SymbolBetween, []interface{}{1, 2})
			query.AddSortN("ID", true)
			var found []storageParityFact
			require.NoError(t, action.Load(query, &found))
			require.EqualValues(t, 2, query.Total)
			require.Len(t, found, 1)
			require.Equal(t, uint(2), found[0].ID)
			found[0].Enabled = false
			require.NoError(t, action.Update(&found[0]))
			query = &types.SearchItem{Model: &storageParityFact{}, IsStatistical: true, StatField: "Amount"}
			var sum decimal.Decimal
			require.NoError(t, action.Load(query, &sum))
			require.Equal(t, "31.25", sum.String())
			tx := action.(interface{ Clone() types.IDataAction }).Clone()
			require.NoError(t, tx.Transaction())
			require.NoError(t, tx.Insert(&storageParityFact{ID: 3, CreatedAt: day, Amount: decimal.NewFromInt(100)}))
			require.NoError(t, tx.Rollback())
			for grain, bucket := range map[TimeGrain]string{GrainDay: "2021-01-01", GrainWeek: "2020-W53", GrainMonth: "2021-01", GrainQuarter: "2021-Q1", GrainYear: "2021"} {
				spec := StatSpec{Code: "parity", Fact: &storageParityFact{}, Grain: grain, Metrics: []StatMetric{{Kind: MetricCount}, {Kind: MetricSum, Field: "Amount"}, {Kind: MetricAvg, Field: "Amount"}}}
				rows, err := Exec(context.Background(), action, spec, ExecOptions{Range: QueryRange{From: day.Add(-time.Hour), To: day.Add(time.Hour)}})
				require.NoError(t, err)
				require.Len(t, rows, 1)
				require.Equal(t, bucket, rows[0].Bucket)
				require.Equal(t, map[string]string{"row_count": "2", "sum_amount": "31.25", "avg_amount": "15.625"}, rows[0].Metrics)
			}
		})
	}
}
