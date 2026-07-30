package stats

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

type testProduct struct {
	*entity.Model
	Code string
	Name string
}

func (testProduct) TableName() string { return "test_products" }

type testOrder struct {
	*entity.Model
	ProductID   uint
	ProductCode string
	ProductName string
	TotalAmount decimal.Decimal
}

func (testOrder) TableName() string { return "test_orders" }

// gormAction 是仅供统计 Exec 使用的最小 IDataAction。
type gormAction struct {
	db *gorm.DB
}

func (a *gormAction) Transaction() error                          { return nil }
func (a *gormAction) Load(*types.SearchItem, interface{}) error   { return nil }
func (a *gormAction) Insert(interface{}) error                    { return nil }
func (a *gormAction) Update(interface{}) error                    { return nil }
func (a *gormAction) Delete(interface{}) error                    { return nil }
func (a *gormAction) Raw(string, interface{}) error               { return nil }
func (a *gormAction) Exec(string, interface{}) error              { return nil }
func (a *gormAction) GetModelDB(interface{}) (interface{}, error) { return a.db, nil }
func (a *gormAction) Commit() error                               { return nil }
func (a *gormAction) GetRunDB() interface{}                       { return a.db }
func (a *gormAction) Rollback() error                             { return nil }

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:stats-test-"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&testProduct{}, &testOrder{}))
	return db
}

func TestValidateDefaultDisplayName(t *testing.T) {
	spec := StatSpec{
		Code:  "t.order.day_product",
		Fact:  &testOrder{},
		Grain: GrainDay,
		Dimensions: []StatDimension{{
			Field:     "ProductID",
			BaseModel: &testProduct{},
		}},
		Metrics: []StatMetric{{Kind: MetricCount}},
	}
	require.NoError(t, Validate(spec))
	require.Equal(t, []string{"Name"}, spec.Dimensions[0].ResolvedDisplayFields())
}

func TestValidateRejectsMissingDisplayField(t *testing.T) {
	spec := StatSpec{
		Code:  "t.bad",
		Fact:  &testOrder{},
		Grain: GrainDay,
		Dimensions: []StatDimension{{
			Field:         "ProductID",
			BaseModel:     &testProduct{},
			DisplayFields: []string{"Title"},
		}},
		Metrics: []StatMetric{{Kind: MetricCount}},
	}
	require.Error(t, Validate(spec))
}

func TestExecDayProductAndRefreshStore(t *testing.T) {
	db := openTestDB(t)
	day := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)

	p1 := &testProduct{Model: entity.NewModel(), Code: "P1", Name: "商品一"}
	p1.ID = 1
	p1.SetHashcode("product-1")
	p2 := &testProduct{Model: entity.NewModel(), Code: "P2", Name: "商品二"}
	p2.ID = 2
	p2.SetHashcode("product-2")
	require.NoError(t, db.Create(p1).Error)
	require.NoError(t, db.Create(p2).Error)

	orders := []*testOrder{
		{Model: entity.NewModel(), ProductID: 1, ProductCode: "P1", ProductName: "商品一", TotalAmount: decimal.NewFromInt(100)},
		{Model: entity.NewModel(), ProductID: 1, ProductCode: "P1", ProductName: "商品一", TotalAmount: decimal.NewFromInt(50)},
		{Model: entity.NewModel(), ProductID: 2, ProductCode: "P2", ProductName: "商品二", TotalAmount: decimal.NewFromInt(20)},
	}
	for i, o := range orders {
		ts := day
		o.CreatedAt = &ts
		o.SetHashcode(fmt.Sprintf("order-%d", i+1))
		require.NoError(t, db.Create(o).Error)
	}

	spec := StatSpec{
		Code:      "t.order.by_day_product",
		Fact:      &testOrder{},
		TimeField: "CreatedAt",
		Grain:     GrainDay,
		Title:     "按天×商品",
		Dimensions: []StatDimension{{
			Field:           "ProductID",
			Alias:           "product",
			BaseModel:       &testProduct{},
			DisplayFromFact: []string{"ProductCode", "ProductName"},
		}},
		Metrics: []StatMetric{
			{Kind: MetricCount, Alias: "row_count"},
			{Kind: MetricSum, Field: "TotalAmount", Alias: "amount_sum"},
		},
	}
	require.NoError(t, Validate(spec))

	action := &gormAction{db: db}
	store := NewStore()
	snap, err := Refresh(context.Background(), store, action, spec, ExecOptions{
		Dialect: DialectSQLite,
		Range: QueryRange{
			From: day.Add(-time.Hour),
			To:   day.Add(24 * time.Hour),
		},
	})
	require.NoError(t, err)
	require.Equal(t, "t.order.by_day_product", snap.Code)
	require.Len(t, snap.Rows, 2)

	byProduct := map[uint]StatRow{}
	for _, row := range snap.Rows {
		require.Equal(t, "2026-07-15", row.Bucket)
		dim := row.Dims["product"]
		byProduct[dim.ID] = row
		// 事实表展示
		require.NotEmpty(t, dim.Displays["productCode"])
		// BaseModel 默认 Name
		require.NotEmpty(t, dim.Displays["name"])
	}

	r1 := byProduct[1]
	require.Equal(t, "2", r1.Metrics["row_count"])
	require.Equal(t, "150", r1.Metrics["amount_sum"])
	require.Equal(t, "商品一", r1.Dims["product"].Displays["name"])
	require.Equal(t, "P1", r1.Dims["product"].Displays["productCode"])

	r2 := byProduct[2]
	require.Equal(t, "1", r2.Metrics["row_count"])
	require.Equal(t, "20", r2.Metrics["amount_sum"])

	got, ok := store.Get(spec.Code)
	require.True(t, ok)
	require.Len(t, got.Rows, 2)
}

func TestRegisterAndGet(t *testing.T) {
	ResetRegistryForTest()
	t.Cleanup(ResetRegistryForTest)
	spec := StatSpec{
		Code:    "reg.demo",
		Fact:    &testOrder{},
		Grain:   GrainMonth,
		Metrics: []StatMetric{{Kind: MetricCount}},
	}
	Register(spec)
	got, ok := Get("reg.demo")
	require.True(t, ok)
	require.Equal(t, GrainMonth, got.Grain)
	require.Equal(t, "CreatedAt", got.TimeField)
}

func TestResolveTableNameUsesGormSchemaNotPluralDefault(t *testing.T) {
	// 模拟框架 OLTP：SingularTable=true → Order 类名为 "order" 而非 "orders"
	db, err := gorm.Open(sqlite.Open("file:stats-singular-"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
		Logger:         logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	type Order struct {
		*entity.Model
		TotalAmount decimal.Decimal
	}
	require.NoError(t, db.AutoMigrate(&Order{}))

	name, err := resolveTableName(db, &Order{})
	require.NoError(t, err)
	require.Equal(t, "order", name, "必须跟连接 NamingStrategy，不能写死复数 orders")

	// 显式 TableName 优先
	name, err = resolveTableName(db, &testOrder{})
	require.NoError(t, err)
	require.Equal(t, "test_orders", name)
}
