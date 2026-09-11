// 本文件用隔离真实 MySQL 验证示例 07 批量确认的提交、回滚和幂等。
package transaction

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/internal/store"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/stretchr/testify/require"
)

type secondUpdateFails struct {
	persistencetypes.IDataAction
	updates int
}

func (a *secondUpdateFails) Update(item interface{}) error {
	a.updates++
	if a.updates == 2 {
		return errors.New("注入第二条确认失败")
	}
	return a.IDataAction.Update(item)
}

// TestOutboxBatchMySQLAtomicity 验证真实事务部分更新失败全部回滚，随后可整批重试。
func TestOutboxBatchMySQLAtomicity(t *testing.T) {
	addr := os.Getenv("CORE_OUTBOX_MYSQL_ADDR")
	if addr == "" {
		t.Skip("NOT RUN: 需要专用 CORE_OUTBOX_MYSQL_ADDR，禁止指向应用数据库")
	}
	host, port, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	t.Setenv("SHOP_ORDER_REMOTE_MYSQL_HOST", host)
	t.Setenv("SHOP_ORDER_REMOTE_MYSQL_PORT", port)
	t.Setenv("SHOP_ORDER_REMOTE_MYSQL_USER", "root")
	t.Setenv("SHOP_ORDER_REMOTE_MYSQL_PASSWORD", "")
	t.Setenv("SHOP_ORDER_REMOTE_MYSQL_DSN", "")
	t.Setenv("SHOP_ORDER_REMOTE_MYSQL_DATABASE", fmt.Sprintf("core_outbox_%d", time.Now().UnixNano()))
	require.NoError(t, store.EnsureRemoteModel(NewOutbox()))
	var ids []uint
	var messages []event.OutboxMessage
	require.NoError(t, store.RunRemoteTransaction(func() error { return nil }, func(action persistencetypes.IDataAction) error {
		for i := 0; i < 3; i++ {
			item := newBatchTestOutbox(fmt.Sprintf("atomic-%d-%d", time.Now().UnixNano(), i))
			if err := item.InsertWith(action); err != nil {
				return err
			}
			ids = append(ids, item.ID)
			messages = append(messages, event.OutboxMessage{ID: item.ID, EventID: item.EventID})
		}
		return nil
	}))
	readPending := func() []*OutboxRecord {
		var items []*OutboxRecord
		query := store.NewSearch(NewOutbox(), len(ids))
		query.AddWhereNS("ID", persistencetypes.SymbolIn, ids)
		require.NoError(t, store.GetRemote().Load(query, &items))
		return items
	}
	require.Len(t, readPending(), 3)
	err = store.RunRemoteTransaction(func() error { return nil }, func(action persistencetypes.IDataAction) error {
		return MarkOutboxPublishedByIDs(&secondUpdateFails{IDataAction: action}, ids)
	})
	require.EqualError(t, err, "注入第二条确认失败")
	for _, item := range readPending() {
		require.False(t, item.Published)
	}
	adapter := OutboxStore{}
	invalid := append(append([]event.OutboxMessage(nil), messages...), event.OutboxMessage{ID: ^uint(0) >> 1})
	require.Error(t, adapter.MarkPublishedBatch(context.Background(), invalid))
	for _, item := range readPending() {
		require.False(t, item.Published)
	}
	require.NoError(t, adapter.MarkPublishedBatch(context.Background(), messages))
	for _, item := range readPending() {
		require.True(t, item.Published)
	}
	require.NoError(t, adapter.MarkPublishedBatch(context.Background(), messages))
	require.NoError(t, adapter.MarkPublishedBatch(context.Background(), nil))
}
