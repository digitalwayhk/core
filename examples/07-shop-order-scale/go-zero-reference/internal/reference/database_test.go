// 本文件用显式提供的隔离 MySQL schema 验证原生事务回滚及失败保留；默认跳过外部依赖。
package reference

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestNativeTransactionRollbackKeepsPending 验证事实已写入事务但事件构造失败时，SQL 回滚且本地事实仍可恢复。
func TestNativeTransactionRollbackKeepsPending(t *testing.T) {
	if os.Getenv("CORE_REFERENCE_TEST_MYSQL") != "1" {
		t.Skip("需要显式隔离 MySQL schema")
	}
	db, e := openDatabase("order", "")
	require.NoError(t, e)
	pool, e := db.DB()
	require.NoError(t, e)
	defer pool.Close()
	s, e := openPending(t.TempDir())
	require.NoError(t, e)
	defer s.close()
	now := time.Now().UTC()
	id := uint(now.UnixNano() >> 8)
	o := testOrder(id, "rollback-"+strconv.FormatUint(uint64(id), 10))
	o.CreatedAt = &now
	o.UpdatedAt = &now
	o.AcceptedAt = now
	_, e = s.accept(context.Background(), o)
	require.NoError(t, e)
	items, e := s.batch(512)
	require.NoError(t, e)
	a := &application{db: db, clock: clock{last: now.UnixMilli() - 1577836800000 + 60000}}
	require.ErrorContains(t, a.syncBatch(context.Background(), items), "时钟回退")
	var count int64
	require.NoError(t, db.Model(&Order{}).Where("id = ?", id).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Model(&OutboxRecord{}).Where("event_id = ?", "order:"+strconv.FormatUint(uint64(id), 10)+":OrderCreated").Count(&count).Error)
	require.Zero(t, count)
	require.EqualValues(t, 1, s.depth.Load())
	a.clock = clock{worker: 3}
	require.NoError(t, a.syncBatch(context.Background(), items))
	require.NoError(t, a.syncBatch(context.Background(), items))
	require.NoError(t, db.Model(&Order{}).Where("id = ?", id).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Model(&OutboxRecord{}).Where("event_id = ?", "order:"+strconv.FormatUint(uint64(id), 10)+":OrderCreated").Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, a.verifyOrphans([]queueOrphan{{id: id, dataKey: localKey(o)}}))
	require.Error(t, a.verifyOrphans([]queueOrphan{{id: id, dataKey: []byte("p:wrong-owner")}}))
	require.NoError(t, s.confirm(items))
	require.Zero(t, s.depth.Load())
}
