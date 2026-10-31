// 本文件在显式专用 MySQL 中只杀死测试自己持有的物理连接，验证不会驱逐共享池或重放写入。
package oltp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type poolRecoveryFact struct {
	ID    uint `gorm:"primaryKey"`
	Value string
}

// TestMySQLPhysicalFailureKeepsOtherTransaction 验证物理连接断开后共享池仍可用，另一事务可提交且写入错误不被重放。
func TestMySQLPhysicalFailureKeepsOtherTransaction(t *testing.T) {
	base := newPoolRecoveryAdapter(t)
	var facts []*poolRecoveryFact
	pool, err := base.db.DB()
	require.NoError(t, err)
	other := base.Clone().(*MySQL)
	require.NoError(t, other.Transaction())
	defer other.Rollback()
	require.NoError(t, other.Insert(&poolRecoveryFact{Value: "另一事务必须提交"}))
	var otherID uint64
	require.NoError(t, other.tx.Raw("SELECT CONNECTION_ID()").Scan(&otherID).Error)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	victim, err := pool.Conn(ctx)
	require.NoError(t, err)
	defer victim.Close()
	var victimID uint64
	require.NoError(t, victim.QueryRowContext(ctx, "SELECT CONNECTION_ID()").Scan(&victimID))
	require.NotEqual(t, otherID, victimID)
	_, err = pool.ExecContext(ctx, fmt.Sprintf("KILL CONNECTION %d", victimID))
	require.NoError(t, err)
	_, physicalErr := victim.ExecContext(ctx, "SELECT 1")
	require.Error(t, physicalErr)
	require.True(t, isConnectionError(physicalErr))
	_ = victim.Close()
	replays := 0
	err = base.errorHandler(physicalErr, &poolRecoveryFact{}, func(*gorm.DB, interface{}) error { replays++; return nil })
	require.True(t, errors.Is(err, physicalErr))
	require.Zero(t, replays)
	require.NoError(t, other.Commit())
	require.NoError(t, pool.PingContext(ctx), "一个物理连接失效不能关闭共用 SQL 池")
	require.NoError(t, base.Load(&types.SearchItem{Model: &poolRecoveryFact{}, Page: 1, Size: 10, SkipCount: true}, &facts))
	require.Len(t, facts, 1)
	require.Equal(t, "另一事务必须提交", facts[0].Value)
}

func newPoolRecoveryAdapter(t *testing.T) *MySQL {
	t.Helper()
	address := os.Getenv("CORE_POOL_MYSQL_ADDR")
	if address == "" {
		t.Skip("需要显式专用 CORE_POOL_MYSQL_ADDR")
	}
	host, portText, err := net.SplitHostPort(address)
	require.NoError(t, err)
	port, err := strconv.Atoi(portText)
	require.NoError(t, err)
	base := NewMySQL(&Config{Host: host, Port: port, Username: "root", Password: os.Getenv("CORE_POOL_MYSQL_PASSWORD"), Database: fmt.Sprintf("core_pool_recovery_%d", time.Now().UnixNano()), Charset: "utf8mb4", ParseTime: true, Loc: "UTC", MaxOpenConns: 8, MaxIdleConns: 4})
	var facts []*poolRecoveryFact
	require.NoError(t, base.Load(&types.SearchItem{Model: &poolRecoveryFact{}, Page: 1, Size: 1, SkipCount: true}, &facts))
	t.Cleanup(func() { connManager.Remove(base.getConnectionKey()) })
	return base
}

// TestMySQLStaleCloneStartsOnReplacementPool 验证旧 Clone 在显式换池后能开始新的事务，而非继续使用已关闭的旧池。
func TestMySQLStaleCloneStartsOnReplacementPool(t *testing.T) {
	base := newPoolRecoveryAdapter(t)
	stale := base.Clone().(*MySQL)
	require.NoError(t, base.RecreateConnection())
	require.NoError(t, stale.Transaction())
	defer stale.Rollback()
	require.NoError(t, stale.Insert(&poolRecoveryFact{Value: "新事务绑定当前池"}))
	require.NoError(t, stale.Commit())
}

// TestMySQLClosedPoolBeginFailureIsEvicted 验证 Begin 失败不重放业务，但后续新事务可恢复且保留原因。
func TestMySQLClosedPoolBeginFailureIsEvicted(t *testing.T) {
	base := newPoolRecoveryAdapter(t)
	pool, err := base.db.DB()
	require.NoError(t, err)
	require.NoError(t, pool.Close())
	first := base.Clone().(*MySQL)
	require.NoError(t, first.Transaction())
	_, err = first.GetModelDB(&poolRecoveryFact{})
	require.Error(t, err)
	require.ErrorContains(t, err, "sql: database is closed")
	_, cached := connManager.GetConnection(base.getConnectionKey())
	require.False(t, cached, "已关闭的池不能继续留在共享注册表")
	next := base.Clone().(*MySQL)
	require.NoError(t, next.Transaction())
	defer next.Rollback()
	require.NoError(t, next.Insert(&poolRecoveryFact{Value: "调用方开启新事务"}))
	require.NoError(t, next.Commit())
}
