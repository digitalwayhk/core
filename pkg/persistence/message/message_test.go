// 本文件用真实 SQLite 验证消息事务回滚、业务记录复用和消费重放，不引入固定消息表。
package message_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/persistence/message"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/stretchr/testify/require"
)

type messageFact struct {
	*entity.Model
	EventID     string `gorm:"type:varchar(191);uniqueIndex"`
	Value       int
	Published   bool
	PublishedAt *time.Time
}

func newFact(id string) *messageFact {
	f := &messageFact{Model: entity.NewModel(), EventID: id}
	f.SetHashcode(id)
	return f
}

func setup(t *testing.T) types.IDataAction {
	t.Helper()
	a := oltp.NewFixedSqlite(fmt.Sprintf("message_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() { require.NoError(t, a.DeleteDB()) })
	var rows []*messageFact
	require.NoError(t, a.Load(&types.SearchItem{Model: newFact(""), Page: 1, Size: 1, SkipCount: true}, &rows))
	return a
}

func readFacts(t *testing.T, a types.IDataAction) []*messageFact {
	t.Helper()
	var rows []*messageFact
	require.NoError(t, a.Load(&types.SearchItem{Model: newFact(""), Page: 1, Size: 100, SkipCount: true}, &rows))
	return rows
}

// TestRunRollsBackBusinessAndMessage 验证业务写入后返回错误或 panic，事实和投递意图均不提交。
func TestRunRollsBackBusinessAndMessage(t *testing.T) {
	a := setup(t)
	errInjected := errors.New("业务失败")
	err := message.Run(context.Background(), a, func(tx types.IDataAction) error {
		require.NoError(t, tx.Insert(newFact("failed")))
		return errInjected
	})
	require.ErrorIs(t, err, errInjected)
	require.Empty(t, readFacts(t, a))
	require.Panics(t, func() {
		_ = message.Run(context.Background(), a, func(tx types.IDataAction) error {
			require.NoError(t, tx.Insert(newFact("panic")))
			panic("失败")
		})
	})
	require.Empty(t, readFacts(t, a))
}

// TestConfirmPublishedReusesBusinessRows 验证原业务行即可承载投递状态，且只更新指定状态及时间字段。
func TestConfirmPublishedReusesBusinessRows(t *testing.T) {
	a := setup(t)
	first, second := newFact("one"), newFact("two")
	first.Value, second.Value = 9, 17
	require.NoError(t, a.Insert(first))
	require.NoError(t, a.Insert(second))
	fields := message.PublishedFields{Published: "Published", PublishedAt: "PublishedAt", UpdatedAt: "UpdatedAt"}
	confirm := func(ids []uint) error {
		return message.Run(context.Background(), a, func(tx types.IDataAction) error {
			return message.ConfirmPublishedWith(context.Background(), tx, newFact(""), ids, fields)
		})
	}
	require.Error(t, confirm([]uint{first.ID, second.ID + 999}))
	for _, row := range readFacts(t, a) {
		require.False(t, row.Published)
	}
	require.NoError(t, confirm([]uint{first.ID, second.ID, first.ID}))
	rows := readFacts(t, a)
	require.Len(t, rows, 2, "不额外插入消息行")
	for _, row := range rows {
		require.True(t, row.Published)
		require.NotNil(t, row.PublishedAt)
		if row.ID == first.ID {
			require.Equal(t, 9, row.Value)
		} else {
			require.Equal(t, 17, row.Value)
		}
	}
	stamp := *rows[0].PublishedAt
	require.NoError(t, confirm([]uint{first.ID, second.ID}))
	require.Equal(t, stamp, *readFacts(t, a)[0].PublishedAt)
	require.NoError(t, message.ConfirmPublishedWith(context.Background(), nil, nil, nil, message.PublishedFields{}))
}

// TestInboxFailureAndReplay 验证业务失败不留去重依据，提交后重投不再修改业务。
func TestInboxFailureAndReplay(t *testing.T) {
	a := setup(t)
	calls := 0
	consume := func(fail bool) error {
		receipt := newFact("consumer:inventory:event-1")
		q := &types.SearchItem{}
		q.AddWhereN("EventID", receipt.EventID)
		return message.Run(context.Background(), a, func(tx types.IDataAction) error {
			return message.ConsumeInboxWith(context.Background(), tx, receipt, q, func(tx types.IDataAction) error {
				calls++
				if err := tx.Insert(newFact("inventory-change")); err != nil {
					return err
				}
				if fail {
					return errors.New("事务中断")
				}
				return nil
			})
		})
	}
	require.Error(t, consume(true))
	require.Empty(t, readFacts(t, a))
	require.NoError(t, consume(false))
	require.NoError(t, consume(false))
	require.Equal(t, 2, calls)
	require.Len(t, readFacts(t, a), 2)
}

// TestCancellationBeforeCommitRollsBack 验证业务执行期间取消后不提交事务。
func TestCancellationBeforeCommitRollsBack(t *testing.T) {
	a := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := message.Run(ctx, a, func(tx types.IDataAction) error {
		if err := tx.Insert(newFact("canceled")); err != nil {
			return err
		}
		cancel()
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, readFacts(t, a))
}
