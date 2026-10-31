// 本文件用真实SQL事务和Redis验证提交前无ACK，以及已提交结果未知后的应用幂等。
package ownership_test

import (
	"context"
	"errors"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestOwnedRealSQLCommitUnknownAndRetry(t *testing.T) {
	db := mysqlFenceDatabase(t)
	p, s, m := mysqlFenceBroker(t)
	ctx := context.Background()
	require.NoError(t, p.BindOwnership(ctx, m))
	require.NoError(t, db.Create(&mysqlFenceFixture{Scope: m.Scope, KeyName: "A"}).Error)
	client := redis.NewClient(&redis.Options{Addr: os.Getenv("CORE_TEST_REDIS_ADDR")})
	defer client.Close()
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var calls atomic.Int64
	runtime, err := p.SubscribeOwnedBatch(ctx, mq.OwnedReliableOptions{Manifest: m, InstanceID: "sql", SessionID: "boot", MaxOwned: 1, NoDueTasks: true, Key: func(*mq.Message) (string, error) { return "A", nil }, Activate: func(ctx context.Context, c ownership.Credential) error {
		return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return ownership.ActivateFence(mysqlFenceTransaction{tx}, c) })
	}}, func(ctx context.Context, c ownership.Credential, messages []*mq.Message) error {
		invocation := calls.Add(1)
		err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := ownership.CheckFence(mysqlFenceTransaction{tx}, c); err != nil {
				return err
			}
			var row mysqlFenceFixture
			if err := tx.Where("scope = ? AND key_name = ?", c.Scope, c.Key).Take(&row).Error; err != nil {
				return err
			}
			if invocation == 1 {
				entered <- struct{}{}
				<-release
			}
			if row.LastID == messages[0].ID {
				return nil
			}
			return tx.Model(&row).Updates(map[string]any{"writes": row.Writes + 1, "last_id": messages[0].ID}).Error
		})
		if err == nil && invocation == 1 {
			return errors.New("已实际提交，但模拟应用未确认提交结果")
		}
		return err
	})
	require.NoError(t, err)
	defer runtime.Close(ctx)
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	require.NoError(t, p.Publish(ctx, m.Subjects[0], []byte("fixture"), &mq.PublishOptions{OrderingKey: "original:A"}))
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("实际SQL事务未进入")
	}
	var before mysqlFenceFixture
	require.NoError(t, db.Where("scope = ? AND key_name = ?", m.Scope, "A").Take(&before).Error)
	require.Zero(t, before.Writes)
	stream := m.Scope + ":" + m.Subjects[0]
	require.EqualValues(t, 1, client.XPending(ctx, stream, m.Group).Val().Count, "未提交SQL不能提前ACK")
	close(release)
	require.Eventually(t, func() bool { return client.XPending(ctx, stream, m.Group).Val().Count == 0 }, 3*time.Second, 10*time.Millisecond)
	require.EqualValues(t, 2, calls.Load(), "结果未知必须保留PEL并重投")
	var after mysqlFenceFixture
	require.NoError(t, db.Where("scope = ? AND key_name = ?", m.Scope, "A").Take(&after).Error)
	require.EqualValues(t, 1, after.Writes, "业务事务内的幂等依据防止重复写入")
	c, err := s.Resolve(ctx, m.Scope, "A")
	require.NoError(t, err)
	require.Equal(t, after.Epoch, c.Epoch)
}
