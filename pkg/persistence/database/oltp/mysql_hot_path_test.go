package oltp

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type pingCountingConnector struct {
	pings atomic.Int32
}

func (connector *pingCountingConnector) Connect(context.Context) (driver.Conn, error) {
	return &pingCountingConn{connector: connector}, nil
}

func (*pingCountingConnector) Driver() driver.Driver { return pingCountingDriver{} }

type pingCountingDriver struct{}

func (pingCountingDriver) Open(string) (driver.Conn, error) {
	return nil, driver.ErrSkip
}

type pingCountingConn struct {
	connector *pingCountingConnector
}

func (*pingCountingConn) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (*pingCountingConn) Close() error                        { return nil }
func (*pingCountingConn) Begin() (driver.Tx, error)           { return nil, driver.ErrSkip }
func (conn *pingCountingConn) Ping(context.Context) error {
	conn.connector.pings.Add(1)
	return nil
}

func newPingCountingGorm(t *testing.T) (*gorm.DB, *pingCountingConnector) {
	t.Helper()
	connector := &pingCountingConnector{}
	sqlDB := sql.OpenDB(connector)
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, connector
}

func TestEnsureValidConnectionDoesNotPingCachedPool(t *testing.T) {
	db, connector := newPingCountingGorm(t)
	adapter := NewMySQL(&Config{Database: "hot_path", MaxIdleConns: 1, MaxOpenConns: 1})
	adapter.Name = "hot_path"
	adapter.db = db

	if err := adapter.ensureValidConnection(); err != nil {
		t.Fatal(err)
	}
	if got := connector.pings.Load(); got != 0 {
		t.Fatalf("cached operation performed %d Ping calls, want 0", got)
	}
}

func TestGetDBDoesNotPingConnectionManagerCache(t *testing.T) {
	db, connector := newPingCountingGorm(t)
	adapter := NewMySQL(&Config{
		Host: fmt.Sprintf("cached-%d", time.Now().UnixNano()), Port: 3306, Database: "hot_path",
		MaxIdleConns: 1, MaxOpenConns: 1,
	})
	adapter.Name = "hot_path"
	key := adapter.getConnectionKey()
	connManager.SetConnection(key, db)
	t.Cleanup(func() { connManager.Remove(key) })

	got, err := adapter.GetDB()
	if err != nil {
		t.Fatal(err)
	}
	if got != db {
		t.Fatalf("GetDB returned %p, want cached %p", got, db)
	}
	if count := connector.pings.Load(); count != 0 {
		t.Fatalf("cached GetDB performed %d Ping calls, want 0", count)
	}
}

func TestConnectionErrorIncludesClosedDatabaseAndDriverSentinels(t *testing.T) {
	for _, err := range []error{
		driver.ErrBadConn,
		sql.ErrConnDone,
		gorm.ErrInvalidDB,
		errors.New("sql: database is closed"),
		errors.New("write tcp: connection reset by peer"),
	} {
		if !isConnectionError(err) {
			t.Fatalf("error %q must be classified as connection error", err)
		}
	}
	if isConnectionError(gorm.ErrRecordNotFound) {
		t.Fatal("business errors must not be classified as connection errors")
	}
}

func TestRetryReadAfterConnectionErrorRefreshesAndRetriesOnce(t *testing.T) {
	refreshCalls := 0
	retryCalls := 0
	err := retryReadAfterConnectionError(
		errors.New("sql: database is closed"),
		false,
		func() error { refreshCalls++; return nil },
		func() error { retryCalls++; return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if refreshCalls != 1 || retryCalls != 1 {
		t.Fatalf("refresh=%d retry=%d want=1/1", refreshCalls, retryCalls)
	}
}

func TestRetryReadAfterConnectionErrorNeverRetriesTransaction(t *testing.T) {
	want := errors.New("bad connection")
	refreshCalls := 0
	retryCalls := 0
	err := retryReadAfterConnectionError(want, true, func() error {
		refreshCalls++
		return nil
	}, func() error {
		retryCalls++
		return nil
	})
	if !errors.Is(err, want) {
		t.Fatalf("error=%v want original transaction error", err)
	}
	if refreshCalls != 0 || retryCalls != 0 {
		t.Fatalf("transaction refresh=%d retry=%d want=0/0", refreshCalls, retryCalls)
	}
}

func TestRetryReadAfterConnectionErrorPreservesRefreshFailure(t *testing.T) {
	original := errors.New("sql: database is closed")
	refreshFailure := errors.New("mysql unavailable")
	retryCalls := 0
	err := retryReadAfterConnectionError(original, false, func() error {
		return refreshFailure
	}, func() error {
		retryCalls++
		return nil
	})
	if !errors.Is(err, original) || !errors.Is(err, refreshFailure) {
		t.Fatalf("error=%v must preserve original and refresh failures", err)
	}
	if retryCalls != 0 {
		t.Fatalf("retry called %d times after refresh failure, want 0", retryCalls)
	}
}

func TestRetryReadAfterConnectionErrorReturnsSingleRetryFailure(t *testing.T) {
	retryFailure := errors.New("read retry failed")
	retryCalls := 0
	err := retryReadAfterConnectionError(errors.New("bad connection"), false, func() error {
		return nil
	}, func() error {
		retryCalls++
		return retryFailure
	})
	if !errors.Is(err, retryFailure) {
		t.Fatalf("error=%v want retry failure", err)
	}
	if retryCalls != 1 {
		t.Fatalf("retry called %d times, want 1", retryCalls)
	}
}

func TestInvalidateConnectionClearsLocalAndSharedReferences(t *testing.T) {
	db, _ := newPingCountingGorm(t)
	adapter := NewMySQL(&Config{Host: fmt.Sprintf("stale-%d", time.Now().UnixNano()), Port: 3306, Database: "hot_path"})
	adapter.Name = "hot_path"
	adapter.db = db
	key := adapter.getConnectionKey()
	connManager.SetConnection(key, db)

	adapter.invalidateConnection()
	if adapter.db != nil {
		t.Fatal("local stale database reference was not cleared")
	}
	if _, ok := connManager.GetConnection(key); ok {
		t.Fatal("shared stale database reference was not cleared")
	}
}

func TestInvalidateStaleCloneDoesNotEvictNewerSharedPool(t *testing.T) {
	staleDB, _ := newPingCountingGorm(t)
	currentDB, _ := newPingCountingGorm(t)
	adapter := NewMySQL(&Config{Host: fmt.Sprintf("stale-clone-%d", time.Now().UnixNano()), Port: 3306, Database: "hot_path"})
	adapter.Name = "hot_path"
	adapter.db = staleDB.Session(&gorm.Session{NewDB: true})
	key := adapter.getConnectionKey()
	connManager.SetConnection(key, currentDB)
	t.Cleanup(func() { connManager.Remove(key) })

	adapter.invalidateConnection()
	if adapter.db != nil {
		t.Fatal("stale clone local database reference was not cleared")
	}
	got, ok := connManager.GetConnection(key)
	if !ok || got != currentDB {
		t.Fatal("stale clone evicted the newer shared connection pool")
	}
}

func TestConnectionManagerGetOrCreatePublishesOnePoolUnderConcurrency(t *testing.T) {
	manager := NewConnectionManager()
	db, _ := newPingCountingGorm(t)
	var creates atomic.Int32
	const callers = 32
	results := make(chan *gorm.DB, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := manager.GetOrCreate("shared", func() (*gorm.DB, error) {
				creates.Add(1)
				return db, nil
			})
			results <- got
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for got := range results {
		if got != db {
			t.Fatalf("GetOrCreate returned %p, want shared %p", got, db)
		}
	}
	if got := creates.Load(); got != 1 {
		t.Fatalf("connection factory called %d times, want 1", got)
	}
}

func TestCleanupExpiredLeavesHealthyServicePoolOpen(t *testing.T) {
	manager := NewConnectionManager()
	db, connector := newPingCountingGorm(t)
	manager.connections["service"] = &ConnectionInfo{
		DB: db, CreatedAt: time.Now().Add(-time.Hour), LastUsed: time.Now().Add(-time.Hour),
	}

	manager.CleanupExpired()
	if got, ok := manager.GetConnection("service"); !ok || got != db {
		t.Fatal("CleanupExpired removed a healthy long-lived service pool")
	}
	if got := connector.pings.Load(); got != 0 {
		t.Fatalf("CleanupExpired pinged a healthy service pool %d times, want 0", got)
	}
}

func TestWriteConnectionErrorInvalidatesWithoutReplay(t *testing.T) {
	db, _ := newPingCountingGorm(t)
	adapter := NewMySQL(&Config{Host: fmt.Sprintf("write-stale-%d", time.Now().UnixNano()), Port: 3306, Database: "hot_path"})
	adapter.Name = "hot_path"
	adapter.db = db
	connManager.SetConnection(adapter.getConnectionKey(), db)
	want := errors.New("sql: database is closed")
	replayCalls := 0
	err := adapter.errorHandler(want, struct{}{}, func(*gorm.DB, interface{}) error {
		replayCalls++
		return nil
	})
	if !errors.Is(err, want) {
		t.Fatalf("error=%v want original write error", err)
	}
	if replayCalls != 0 {
		t.Fatalf("write replayed %d times, want 0", replayCalls)
	}
	if adapter.db != nil {
		t.Fatal("write connection error did not invalidate local reference")
	}
}
