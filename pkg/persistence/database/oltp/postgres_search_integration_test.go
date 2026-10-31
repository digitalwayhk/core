//go:build integration

// 本文件验证 PostgreSQL 标准查询、空值、自定义列及大批量绑定参数。
package oltp

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type pgSearchRecord struct {
	ID       uint   `gorm:"primaryKey;autoIncrement:false"`
	Label    string `gorm:"column:order"`
	Amount   decimal.Decimal
	Enabled  bool
	Optional sql.NullString
}

type pgDisplayBase struct {
	ID       uint `gorm:"primaryKey"`
	Name     string
	Database string `gorm:"-"`
}

func (m *pgDisplayBase) GetRemoteDBName() string { return m.Database }
func (*pgDisplayBase) GetLocalDBName() string    { return "" }

type pgDisplayFact struct {
	ID        uint `gorm:"primaryKey"`
	BaseID    uint `gorm:"column:base_ref"`
	Snapshot  string
	CreatedAt time.Time
	Database  string `gorm:"-"`
}

func (m *pgDisplayFact) GetRemoteDBName() string { return m.Database }
func (*pgDisplayFact) GetLocalDBName() string    { return "" }

// TestPostgresIntegration_CrossDatabaseDisplay 验证基础资料从其模型库解析，事实快照和维度分组不跨库Join。
func TestPostgresIntegration_CrossDatabaseDisplay(t *testing.T) {
	c := postgresIntegrationConfig(t)
	c.Database = ""
	p := NewPostgres(c)
	prefix := fmt.Sprintf("core_pg_display_%d", time.Now().UnixNano())
	base := &pgDisplayBase{ID: 1, Name: "基础资料名称", Database: prefix + "_base"}
	fact := &pgDisplayFact{BaseID: 1, Snapshot: "历史名称", CreatedAt: time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC), Database: prefix + "_fact"}
	require.NoError(t, p.HasTable(base))
	require.NoError(t, p.HasTable(fact))
	require.NoError(t, p.Insert(base))
	require.NoError(t, p.Insert(fact))
	rows, err := Aggregate(context.Background(), p, types.AggregateQuery{Fact: fact, Grain: types.GrainDay, Dimensions: []types.AggregateDimension{{Field: "BaseID", BaseModel: base, DisplayFromFact: []string{"Snapshot"}}}, Metrics: []types.AggregateMetric{{Kind: types.MetricCount}}})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "基础资料名称", rows[0].Dims["base"].Displays["name"])
	require.Equal(t, "历史名称", rows[0].Dims["base"].Displays["snapshot"])
}

func (*pgSearchRecord) TableName() string { return "pg_search_contract" }

// TestPostgresIntegration_SearchAndBatch 验证通用SearchItem，不要求业务编写PG专用SQL。
func TestPostgresIntegration_SearchAndBatch(t *testing.T) {
	c := postgresIntegrationConfig(t)
	c.Database += "_search"
	p := NewPostgres(c)
	require.NoError(t, p.HasTable(&pgSearchRecord{}))
	rows := make([]pgSearchRecord, 14000)
	for i := range rows {
		rows[i] = pgSearchRecord{ID: uint(i + 1), Label: "item", Amount: decimal.NewFromInt(2), Enabled: i%2 == 0}
	}
	require.NoError(t, p.Insert(&rows))
	query := &types.SearchItem{Model: &pgSearchRecord{}, Page: 2, Size: 2}
	query.AddWhereN("Label", "item")
	query.AddWhereN("Enabled", true)
	query.AddWhereNS("ID", types.SymbolBetween, []interface{}{1, 10})
	query.AddWhereNS("Optional", types.SymbolIsNull, nil)
	query.AddSortN("ID", false)
	var result []pgSearchRecord
	require.NoError(t, p.Load(query, &result))
	require.EqualValues(t, 5, query.Total)
	require.Equal(t, uint(5), result[0].ID)
	require.Equal(t, uint(7), result[1].ID)
	query = &types.SearchItem{Model: &pgSearchRecord{}, Size: 10}
	query.AddWhereNS("ID", types.SymbolIn, []uint{1, 3})
	result = nil
	require.NoError(t, p.Load(query, &result))
	require.Len(t, result, 2)
	query = &types.SearchItem{Model: &pgSearchRecord{}, IsStatistical: true, StatField: "Amount"}
	query.AddWhereNS("ID", types.SymbolIn, []uint{1, 3})
	var total decimal.Decimal
	require.NoError(t, p.Load(query, &total))
	require.Equal(t, "4", total.String())
}
