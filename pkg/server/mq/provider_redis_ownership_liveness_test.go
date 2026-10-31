// 本文件以真实Redis和加速时钟验证pending为零时的未读/到期/未知观测，空闲不换主。
package mq

import (
	"context"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestRedisOwnershipLivenessWithoutPending(t *testing.T) {
	for _, kind := range []string{"unread", "due", "unknown", "idle"} {
		t.Run(kind, func(t *testing.T) {
			p := newPoolBudgetProvider(t, 4, 8)
			ctx := context.Background()
			m := ownership.Manifest{Scope: kind, Service: "quotes", Group: "quotes", Selector: "v1", Keys: []string{"A"}, Subjects: []string{"one"}, Timing: ownership.Timing{Lease: 900 * time.Millisecond, Renew: 100 * time.Millisecond, Poll: 10 * time.Millisecond, Execution: 300 * time.Millisecond, Stall: 500 * time.Millisecond, Observation: 250 * time.Millisecond}}.Normalize()
			require.NoError(t, p.ProvisionOwnership(ctx, m, 0))
			require.NoError(t, p.BindOwnership(ctx, m))
			s, err := p.OwnershipStore(ctx)
			require.NoError(t, err)
			if kind == "unread" {
				require.NoError(t, p.Publish(ctx, "one", []byte("A"), &PublishOptions{OrderingKey: "A"}))
			}
			options := OwnedReliableOptions{Manifest: m, NoDueTasks: kind != "due", Key: func(*Message) (string, error) { return "A", nil }}
			if kind == "due" {
				options.Due = func(context.Context, ownership.Credential) (int64, error) { return 1, nil }
			}
			reader := newRedisOwnedReader(p, options, nil, &redisReliableSubscriptionMetrics{redisReliableDeliveryMetrics: &p.deliveryMetrics})
			activated := make(chan ownership.Credential, 2)
			runtime, err := ownership.StartRuntime(ctx, s, ownership.Options{Manifest: m, InstanceID: "instance", SessionID: "boot", MaxOwned: 1, Activate: func(_ context.Context, c ownership.Credential) error { activated <- c; return nil }, Step: func(context.Context, ownership.Credential) (bool, error) { return false, nil }, Observe: func(ctx context.Context, c ownership.Credential) (ownership.Observation, error) {
				if kind == "unknown" {
					return ownership.Observation{}, ownership.ErrUnknown
				}
				return reader.observe(ctx, c)
			}})
			require.NoError(t, err)
			defer runtime.Close(ctx)
			var c ownership.Credential
			select {
			case c = <-activated:
			case <-time.After(time.Second):
				t.Fatal("未取得租约")
			}
			require.Eventually(t, func() bool { return s.Validate(ctx, c, true) == nil }, time.Second, time.Millisecond)
			require.Zero(t, p.client.XPending(ctx, p.streamKey("one"), m.Group).Val().Count)
			if kind == "idle" {
				time.Sleep(750 * time.Millisecond)
				require.NoError(t, s.Validate(ctx, c, true))
				status, err := runtime.Status(ctx, "A")
				require.NoError(t, err)
				require.Nil(t, status.LastConfirmed)
			} else {
				require.Eventually(t, func() bool { return s.Validate(ctx, c, true) != nil }, time.Second, 5*time.Millisecond, "有工作停滞或观测失联应撤销健康进程的单key归属")
			}
		})
	}
}
