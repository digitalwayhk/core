package oltp

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/stretchr/testify/require"
)

type mysqlConcurrencyRecord struct {
	ID        uint       `gorm:"primarykey"`
	CreatedAt *time.Time `json:"createdAt"`
	UpdatedAt *time.Time `json:"updatedAt"`
	Hashcode  string     `gorm:"column:hashcode;type:varchar(500);uniqueIndex"`
	DBName    string     `gorm:"-"`
	Key       string     `gorm:"column:test_key;type:varchar(128);uniqueIndex"`
	Value     string     `gorm:"column:test_value;type:varchar(255)"`
}

func newMySQLConcurrencyRecord(dbName, key, value string) *mysqlConcurrencyRecord {
	return &mysqlConcurrencyRecord{
		DBName: dbName,
		Key:    key,
		Value:  value,
	}
}

func (m *mysqlConcurrencyRecord) Equals(other interface{}) bool {
	o, ok := other.(types.IModel)
	if !ok {
		return false
	}
	return m.ID > 0 && o.GetID() > 0 && m.ID == o.GetID()
}

func (m *mysqlConcurrencyRecord) GetID() uint {
	return m.ID
}

func (m *mysqlConcurrencyRecord) SetID(id uint) {
	m.ID = id
}

func (m *mysqlConcurrencyRecord) SetHashcode(code string) {
	m.Hashcode = code
}

func (m *mysqlConcurrencyRecord) GetLocalDBName() string {
	return m.DBName
}

func (m *mysqlConcurrencyRecord) GetRemoteDBName() string {
	return m.DBName
}

func (m *mysqlConcurrencyRecord) GetHash() string {
	return m.Key
}

func newMySQLConcurrencyAdapter(t *testing.T, dbName string) *MySQL {
	t.Helper()

	adapter := NewMySQL(newMySQLConcurrencyConfig())

	t.Cleanup(func() {
		adapter.Name = dbName
		_ = adapter.DeleteDB()
	})

	return adapter
}

func newMySQLConcurrencyConfig() *Config {
	return &Config{
		Host:         "127.0.0.1",
		Port:         3306,
		Username:     "root",
		Password:     "your_password",
		Charset:      "utf8mb4",
		ParseTime:    true,
		Loc:          "Local",
		MaxIdleConns: 5,
		MaxOpenConns: 10,
		MaxLifetime:  30 * time.Minute,
	}
}

func newMySQLConcurrencyDBName(t *testing.T) string {
	t.Helper()

	name := strings.ToLower(t.Name())
	replacer := strings.NewReplacer("/", "_", "-", "_", " ", "_")
	name = replacer.Replace(name)
	if len(name) > 12 {
		name = name[:12]
	}
	return fmt.Sprintf("oc_%s_%d", name, time.Now().UnixNano()%1000000)
}

func fetchMySQLConcurrencyRecord(t *testing.T, dbName, key string) mysqlConcurrencyRecord {
	t.Helper()

	probe := NewMySQL(newMySQLConcurrencyConfig())
	probe.Name = dbName
	db, err := probe.GetDB()
	require.NoError(t, err)

	var got mysqlConcurrencyRecord
	err = db.Where("hashcode = ?", key).First(&got).Error
	require.NoError(t, err)
	got.DBName = dbName
	return got
}

func cleanupMySQLConcurrencyDB(t *testing.T, dbName string) {
	t.Helper()
	adapter := NewMySQL(newMySQLConcurrencyConfig())
	adapter.Name = dbName
	_ = adapter.DeleteDB()
}

func requireConcurrencyReproEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("RUN_MYSQL_CONCURRENCY_REPRO") != "1" {
		t.Skip("设置 RUN_MYSQL_CONCURRENCY_REPRO=1 后再运行该复现测试")
	}
}

func TestMySQL_SharedInstanceLoadSeesOtherGoroutineUncommittedData(t *testing.T) {
	requireConcurrencyReproEnabled(t)

	dbName := newMySQLConcurrencyDBName(t)
	shared := newMySQLConcurrencyAdapter(t, dbName)

	record := newMySQLConcurrencyRecord(dbName, "load-visible-key", "committed")
	require.NoError(t, shared.Insert(record))

	require.NoError(t, shared.Transaction())
	defer func() { _ = shared.Rollback() }()

	record.Value = "uncommitted"
	require.NoError(t, shared.Update(record))

	search := &types.SearchItem{
		Page:  1,
		Size:  10,
		Model: &mysqlConcurrencyRecord{DBName: dbName},
	}
	search.AddWhereN("Hashcode", record.Key)

	resultCh := make(chan []mysqlConcurrencyRecord, 1)
	errCh := make(chan error, 1)

	go func() {
		var rows []mysqlConcurrencyRecord
		errCh <- shared.Load(search, &rows)
		resultCh <- rows
	}()

	require.NoError(t, <-errCh)
	rows := <-resultCh
	require.Len(t, rows, 1)

	if rows[0].Value != "committed" {
		t.Fatalf("共享 MySQL 实例在一个 goroutine 已开启事务后，另一个 goroutine 的 Load 读到了未提交数据: got=%q want=%q", rows[0].Value, "committed")
	}
}

func TestMySQL_SharedInstanceConcurrentUpdateIsRolledBackWithForeignTransaction(t *testing.T) {
	requireConcurrencyReproEnabled(t)

	dbName := newMySQLConcurrencyDBName(t)
	shared := newMySQLConcurrencyAdapter(t, dbName)

	txnRecord := newMySQLConcurrencyRecord(dbName, "txn-key", "before-txn")
	outsideRecord := newMySQLConcurrencyRecord(dbName, "outside-key", "before-outside")
	require.NoError(t, shared.Insert(txnRecord))
	require.NoError(t, shared.Insert(outsideRecord))

	require.NoError(t, shared.Transaction())

	txnRecord.Value = "inside-txn"
	require.NoError(t, shared.Update(txnRecord))

	concurrentUpdate := newMySQLConcurrencyRecord(dbName, outsideRecord.Key, "outside-should-commit")
	var wg sync.WaitGroup
	errCh := make(chan error, 1)

	wg.Add(1)
	go func() {
		defer wg.Done()
		errCh <- shared.Update(concurrentUpdate)
	}()
	wg.Wait()
	require.NoError(t, <-errCh)

	require.NoError(t, shared.Rollback())

	gotTxn := fetchMySQLConcurrencyRecord(t, dbName, txnRecord.Key)
	gotOutside := fetchMySQLConcurrencyRecord(t, dbName, outsideRecord.Key)

	require.Equal(t, "before-txn", gotTxn.Value, "事务内更新在回滚后应该被撤销")
	if gotOutside.Value != "outside-should-commit" {
		t.Fatalf("共享 MySQL 实例把另一个 goroutine 的 Update 误并入当前事务并一并回滚: got=%q want=%q", gotOutside.Value, "outside-should-commit")
	}
}

func TestMySQL_TransactionRejectsDifferentDBName(t *testing.T) {
	dbNameA := newMySQLConcurrencyDBName(t)
	dbNameB := newMySQLConcurrencyDBName(t)
	t.Cleanup(func() { cleanupMySQLConcurrencyDB(t, dbNameA) })
	t.Cleanup(func() { cleanupMySQLConcurrencyDB(t, dbNameB) })

	adapter := NewMySQL(newMySQLConcurrencyConfig())
	require.NoError(t, adapter.Transaction())

	itemA := newMySQLConcurrencyRecord(dbNameA, "tx-bind-a", "value-a")
	require.NoError(t, adapter.Insert(itemA))

	itemB := newMySQLConcurrencyRecord(dbNameB, "tx-bind-b", "value-b")
	err := adapter.Insert(itemB)
	require.Error(t, err)
	require.Contains(t, err.Error(), "transaction is bound to db")

	require.NoError(t, adapter.Rollback())
}
