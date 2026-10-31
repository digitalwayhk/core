// 本文件验证真实MySQL取批的取消传播，并保留未发布过滤、升序和批量边界。
package transaction

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/internal/store"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestOutboxLoadPendingCancellation 验证预取消不读库，锁等待中的查询因deadline结束而非等锁释放。
func TestOutboxLoadPendingCancellation(t *testing.T) {
	if os.Getenv("CORE_TEST_MYSQL") != "1" {
		t.Skip("需要本轮独占临时MySQL")
	}
	require.NoError(t, store.EnsureRemoteModel(NewOutbox()))
	adapter := OutboxStore{}
	t.Run("cancelled_before_query", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := adapter.LoadPending(ctx, 100)
		require.ErrorIs(t, err, context.Canceled)
	})
	t.Run("deadline_during_table_lock", func(t *testing.T) {
		raw, err := store.GetRemote().GetModelDB(NewOutbox())
		require.NoError(t, err)
		db := raw.(*gorm.DB)
		pool, err := db.DB()
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		lock, err := pool.Conn(ctx)
		require.NoError(t, err)
		defer lock.Close()
		stmt := &gorm.Statement{DB: db}
		require.NoError(t, stmt.Parse(NewOutbox()))
		_, err = lock.ExecContext(ctx, "LOCK TABLES `"+stmt.Schema.Table+"` WRITE")
		require.NoError(t, err)
		// 旧实现会一直等到解锁才成功；独立定时解锁避免RED挂死。
		released := make(chan error, 1)
		time.AfterFunc(time.Second, func() { _, err := lock.ExecContext(ctx, "UNLOCK TABLES"); released <- err })
		queryCtx, queryCancel := context.WithTimeout(ctx, 150*time.Millisecond)
		defer queryCancel()
		_, queryErr := adapter.LoadPending(queryCtx, 100)
		require.NoError(t, <-released)
		require.ErrorIs(t, queryErr, context.DeadlineExceeded)
	})
}

// TestOutboxPendingReadContract 验证只取未发布记录，先小ID且实际条数受limit约束。
func TestOutboxPendingReadContract(t *testing.T) {
	if os.Getenv("CORE_TEST_MYSQL") != "1" {
		t.Skip("需要本轮独占临时MySQL")
	}
	require.NoError(t, store.EnsureRemoteModel(NewOutbox()))
	var ids []uint
	require.NoError(t, store.RunRemoteTransaction(func() error { return nil }, func(action persistencetypes.IDataAction) error {
		for i := range 13 {
			item, err := NewOutboxRecord("cancel-test", fmt.Sprintf("cancel-%d-%d", time.Now().UnixNano(), i), "OrderCreated", "shop.order.changed", map[string]uint{"orderID": uint(i + 1)})
			if err != nil {
				return err
			}
			if err = item.InsertWith(action); err != nil {
				return err
			}
			ids = append(ids, item.ID)
		}
		return MarkOutboxPublishedByID(action, ids[0])
	}))
	items, err := (OutboxStore{}).LoadPending(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, ids[1], items[0].ID)
	for _, limit := range []int{0, -1} {
		items, err := (OutboxStore{}).LoadPending(context.Background(), limit)
		require.NoError(t, err)
		require.Len(t, items, 10, "非正值保持原分页默认，不能无界读取")
	}
	legacy, err := PendingOutbox(2)
	require.NoError(t, err)
	require.Len(t, legacy, 2)
	require.Equal(t, ids[1], legacy[0].ID)
}

// TestOutboxLoadPendingRejectsInvalidContext 验证无效上下文在访问数据库前即被拒绝。
func TestOutboxLoadPendingRejectsInvalidContext(t *testing.T) {
	_, err := (OutboxStore{}).LoadPending(nil, 1)
	require.Error(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = (OutboxStore{}).LoadPending(ctx, 1)
	require.ErrorIs(t, err, context.Canceled)
}

// TestOutboxLoadPendingRecoversClosedPool 验证取消接线不绕过原有只读连接恢复。
func TestOutboxLoadPendingRecoversClosedPool(t *testing.T) {
	if os.Getenv("CORE_TEST_MYSQL") != "1" {
		t.Skip("需要本轮独占临时MySQL")
	}
	require.NoError(t, store.EnsureRemoteModel(NewOutbox()))
	raw, err := store.GetRemote().GetModelDB(NewOutbox())
	require.NoError(t, err)
	pool, err := raw.(*gorm.DB).DB()
	require.NoError(t, err)
	require.NoError(t, pool.Close())
	_, err = (OutboxStore{}).LoadPending(context.Background(), 1)
	require.NoError(t, err)
}
