// 本文件用真实SQL待办与原生Redis归属时限验证业务阻挡不等于运行时停滞，不绑定或重建原MQ组。
package ownership_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ownershipSQLTask只用于合成任务的持久分类，不是Core默认业务模型或金融状态机。
type ownershipSQLTask struct {
	ID            uint   `gorm:"primaryKey"`
	OwnerKey      string `gorm:"size:64;index"`
	OrderKey      string `gorm:"size:64;index"`
	Ordinal       int
	State         string `gorm:"size:32"`
	Writes        int64
	Attempts      int64
	WriterSession string `gorm:"size:128"`
}

// blockedDueDeclaration只让同一真实旧版RED可编译；缺字段时不改Due、不伪造计数或跳过测试。
func blockedDueDeclaration(o *ownership.Observation, n int64) bool {
	f := reflect.ValueOf(o).Elem().FieldByName("BlockedDue")
	if !f.IsValid() {
		return false
	}
	if f.Kind() != reflect.Int64 {
		return false
	}
	f.SetInt(n)
	return true
}

func taskObservation(ctx context.Context, db *gorm.DB, key string) (ownership.Observation, error) {
	o := ownership.Observation{At: time.Now()}
	table := db.NamingStrategy.TableName("ownershipSQLTask")
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&ownershipSQLTask{}).Where("owner_key = ? AND state = ?", key, "pending").Count(&o.Due).Error; err != nil {
			return err
		}
		var blocked int64
		q := tx.Table(table+" AS task").Where("task.owner_key = ? AND task.state = ?", key, "pending").Where("EXISTS (SELECT 1 FROM "+table+" AS prior WHERE prior.owner_key = task.owner_key AND prior.order_key = task.order_key AND prior.ordinal < task.ordinal AND prior.state = ?)", "paused")
		if err := q.Count(&blocked).Error; err != nil {
			return err
		}
		blockedDueDeclaration(&o, blocked)
		return nil
	}, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	return o, err
}

func taskStep(ctx context.Context, db *gorm.DB, c ownership.Credential) (bool, error) {
	progress := false
	table := db.NamingStrategy.TableName("ownershipSQLTask")
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ownership.CheckFence(mysqlFenceTransaction{tx}, c); err != nil {
			return err
		}
		var task ownershipSQLTask
		err := tx.Table(table+" AS task").Where("task.owner_key = ? AND task.state = ?", c.Key, "pending").Where("NOT EXISTS (SELECT 1 FROM "+table+" AS prior WHERE prior.owner_key = task.owner_key AND prior.order_key = task.order_key AND prior.ordinal < task.ordinal AND prior.state NOT IN ?)", []string{"done", "resolved"}).Order("task.id").Take(&task).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		result := tx.Model(&ownershipSQLTask{}).Where("id = ? AND state = ?", task.ID, "pending").Updates(map[string]any{"state": "done", "writes": gorm.Expr("writes + 1"), "writer_session": c.SessionID})
		progress = result.RowsAffected == 1
		return result.Error
	})
	return progress, err
}

// TestSQLBlockedDuePreservesOwnerAndHealthyTask使用默认15s/45s，不缩租期，不以零计数或心跳冒充进展。
func TestSQLBlockedDuePreservesOwnerAndHealthyTask(t *testing.T) {
	db := mysqlFenceDatabase(t, &ownershipSQLTask{}).Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT_RUN: isolated CORE_TEST_REDIS_ADDR")
	}
	m := ownership.Manifest{Scope: fmt.Sprintf("task-blocked-%d", time.Now().UnixNano()), Service: "fixture", Group: "fixture", Selector: "opaque-v1", Keys: []string{"A", "B"}, Subjects: []string{"receipt"}}.Normalize()
	p := mq.NewRedisStreamProviderWithConfig(config.RedisStreamMQConfig{Addr: addr, Prefix: m.Scope, CommandPoolSize: 8, BlockingReadPoolSize: 4})
	ctx := context.Background()
	require.NoError(t, p.Connect(ctx))
	defer p.Close()
	require.NoError(t, p.ProvisionOwnership(ctx, m, 0))
	store, err := p.OwnershipStore(ctx)
	require.NoError(t, err)
	for _, key := range m.Keys {
		require.NoError(t, db.Create(&mysqlFenceFixture{Scope: m.Scope, KeyName: key}).Error)
	}
	tasks := []ownershipSQLTask{{OwnerKey: "A", OrderKey: "held", Ordinal: 1, State: "paused"}, {OwnerKey: "A", OrderKey: "held", Ordinal: 2, State: "pending"}}
	require.NoError(t, db.Create(&tasks).Error)
	// 原MQ收单入口的PEL仅作为保全哨兵，不给SQL Runtime绑定该Stream。
	client := redis.NewClient(&redis.Options{Addr: addr})
	defer client.Close()
	stream := m.Scope + ":" + m.Subjects[0]
	require.NoError(t, client.XGroupCreateMkStream(ctx, stream, m.Group, "0").Err())
	require.NoError(t, p.Publish(ctx, m.Subjects[0], []byte("synthetic-receipt"), &mq.PublishOptions{OrderingKey: "receipt-key"}))
	original, err := client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: m.Group, Consumer: "original-receiver", Streams: []string{stream, ">"}, Count: 1}).Result()
	require.NoError(t, err)
	originalID := original[0].Messages[0].ID
	runtime, err := ownership.StartRuntime(ctx, store, ownership.Options{Manifest: m, InstanceID: "owner", SessionID: "boot", MaxOwned: 2, Activate: func(ctx context.Context, c ownership.Credential) error {
		return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return ownership.ActivateFence(mysqlFenceTransaction{tx}, c) })
	}, Step: func(ctx context.Context, c ownership.Credential) (bool, error) { return taskStep(ctx, db, c) }, Observe: func(ctx context.Context, c ownership.Credential) (ownership.Observation, error) {
		return taskObservation(ctx, db, c.Key)
	}})
	require.NoError(t, err)
	defer runtime.Close(ctx)
	var first ownership.Credential
	require.Eventually(t, func() bool { first, err = store.Resolve(ctx, m.Scope, "A"); return err == nil }, 5*time.Second, 10*time.Millisecond)
	before, err := runtime.Status(ctx, "A")
	require.NoError(t, err)
	require.EqualValues(t, 1, before.Observation.Due)
	require.Nil(t, before.LastConfirmed)
	// 固定真实默认stall+一次renew，原子SQL观测持续保留Due=1；不是容易波动的性能毫秒门槛。
	until := time.NewTimer(m.Timing.Stall + 2*m.Timing.Renew)
	<-until.C
	_, idleErr := store.Resolve(ctx, m.Scope, "B")
	require.NoError(t, idleErr, "空闲桶仍应Ready")
	status, err := runtime.Status(ctx, "A")
	require.NoError(t, err, "合法PAUSED前序阻挡不应让整个技术桶冷却")
	require.Equal(t, first.Epoch, status.Epoch)
	require.EqualValues(t, 1, status.Observation.Due)
	require.Nil(t, status.LastConfirmed, "轮询和阻挡分类不是业务确认")
	field := reflect.ValueOf(status.Observation).FieldByName("BlockedDue")
	require.True(t, field.IsValid(), "正式加性字段必须存在")
	require.EqualValues(t, 1, field.Int(), "Status必须返回真实观测的阻挡量，不能测试时补写")
	require.Zero(t, runtime.RuntimeMetricSnapshot(ctx).Counters["ownership_stalled_revoked_total"])
	require.True(t, status.Observation.HasWork(), "阻挡不等于没有未完成原件")
	healthy := ownershipSQLTask{OwnerKey: "A", OrderKey: "healthy", Ordinal: 1, State: "pending"}
	require.NoError(t, db.Create(&healthy).Error)
	require.Eventually(t, func() bool {
		var row ownershipSQLTask
		return db.First(&row, healthy.ID).Error == nil && row.Writes == 1
	}, 3*time.Second, 10*time.Millisecond, "同一技术桶的独立健康组须实际SQL提交")
	var held []ownershipSQLTask
	require.NoError(t, db.Where("id IN ?", []uint{tasks[0].ID, tasks[1].ID}).Order("ordinal").Find(&held).Error)
	require.Equal(t, "paused", held[0].State)
	require.Equal(t, "pending", held[1].State)
	require.Zero(t, held[0].Writes)
	require.Zero(t, held[1].Writes)
	var after ownership.KeyStatus
	require.Eventually(t, func() bool { after, err = runtime.Status(ctx, "A"); return err == nil && after.LastConfirmed != nil }, time.Second, time.Millisecond)
	require.Equal(t, first.Epoch, after.Epoch)
	// 仅测试应用明确解除前序；不是Core跳过或把接管认成业务成功。
	require.NoError(t, db.Model(&ownershipSQLTask{}).Where("id = ?", tasks[0].ID).Update("state", "resolved").Error)
	require.Eventually(t, func() bool {
		var row ownershipSQLTask
		return db.First(&row, tasks[1].ID).Error == nil && row.Writes == 1
	}, 3*time.Second, 10*time.Millisecond)
	pending, err := client.XPendingExt(ctx, &redis.XPendingExtArgs{Stream: stream, Group: m.Group, Start: "-", End: "+", Count: 8}).Result()
	require.NoError(t, err)
	require.Len(t, pending, 1)
	require.Equal(t, originalID, pending[0].ID)
	groups, err := client.XInfoGroups(ctx, stream).Result()
	require.NoError(t, err)
	require.Equal(t, originalID, groups[0].LastDeliveredID)
	t.Logf("default lease=%s/stall=%s: same epoch=%d, true Due retained, no fake confirmation, same-bucket healthy SQL committed, original PEL/cursor retained", m.Timing.Lease, m.Timing.Stall, first.Epoch)
}
