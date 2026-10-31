// 本文件验证正式ServiceContext→EventBridge→MQManager→Redis的多主题批次，不替代业务SQL验证。
package router_test

import (
	"context"
	"fmt"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestServiceContextOwnedEventsPreservesEnvelopeAndRetries(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT_RUN: CORE_TEST_REDIS_ADDR")
	}
	ctx := context.Background()
	name := fmt.Sprintf("owned-chain-%d", time.Now().UnixNano())
	cfg := config.NewServiceDefaultConfig(name, 29991)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "on"
	cfg.MQ.Provider = "redis-stream"
	cfg.MQ.RedisStream = config.RedisStreamMQConfig{Addr: addr, Prefix: name, CommandPoolSize: 4, BlockingReadPoolSize: 4}
	sc := router.NewServiceContextWithConfig(&fakeService{name}, cfg)
	t.Cleanup(func() { sc.SetRunState(false); require.NoError(t, sc.ShutdownError()) })
	m := ownership.Manifest{Scope: name, Service: name, Group: name, Selector: "data-prefix-v1", Keys: []string{"A"}, Subjects: []string{"one", "two"}}.Normalize()
	require.NoError(t, sc.MQManager.ProvisionOwnership(ctx, m, 0))
	require.NoError(t, sc.MQManager.BindOwnership(ctx, m))
	expected := map[string]bool{}
	for _, subject := range m.Subjects {
		for i := 0; i < 4; i++ {
			e := event.NewEnvelope("fixture", "changed", []byte(fmt.Sprintf("A:%d", i)))
			e.Subject = subject
			e.ShardKey = "ordering-A"
			expected[e.ID] = true
			require.NoError(t, sc.EventBridge.Publish(ctx, subject, e))
		}
	}
	var failed atomic.Bool
	received := make(chan []*event.Envelope, 8)
	runtime, err := sc.SubscribeOwnedEvents(event.OwnedSubscription{Manifest: m, MaxOwned: 1, NoDueTasks: true, Key: func(e *event.Envelope) (string, error) { return strings.Split(string(e.Data), ":")[0], nil }, Activate: func(_ context.Context, c ownership.Credential) error { return c.Validate() }, Handler: func(ctx context.Context, c ownership.Credential, rows []*event.Envelope) error {
		if !failed.Swap(true) {
			return fmt.Errorf("fixture: transaction not committed")
		}
		received <- rows
		return nil
	}})
	require.NoError(t, err)
	defer runtime.Close(ctx)
	seen := map[string]bool{}
	multi := false
	for len(seen) < 8 {
		select {
		case rows := <-received:
			multi = multi || len(rows) > 1
			for _, e := range rows {
				require.True(t, expected[e.ID])
				require.False(t, seen[e.ID])
				require.Equal(t, "ordering-A", e.ShardKey)
				seen[e.ID] = true
			}
		case <-time.After(4 * time.Second):
			t.Fatal("正式链未在失败重试后完成原Envelope批次")
		}
	}
	require.True(t, multi)
	require.True(t, failed.Load())
	store, err := sc.MQManager.OwnershipStore(ctx)
	require.NoError(t, err)
	owner, err := store.Resolve(ctx, m.Scope, "A")
	require.NoError(t, err)
	require.Equal(t, sc.ServiceInstanceID, owner.InstanceID)
}

// 验证多个scope共享一个实例级低基数组件，不相互覆盖采集。
func TestServiceContextOwnedMetricsAggregateScopes(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT_RUN: CORE_TEST_REDIS_ADDR")
	}
	name := fmt.Sprintf("owned-metrics-%d", time.Now().UnixNano())
	cfg := config.NewServiceDefaultConfig(name, 29992)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "on"
	cfg.MQ.Provider = "redis-stream"
	cfg.MQ.RedisStream = config.RedisStreamMQConfig{Addr: addr, Prefix: name, CommandPoolSize: 4, BlockingReadPoolSize: 4}
	sc := router.NewServiceContextWithConfig(&fakeService{name}, cfg)
	t.Cleanup(func() { sc.SetRunState(false); require.NoError(t, sc.ShutdownError()) })
	foreign := ownership.Manifest{Scope: name + "-foreign", Service: "another-service", Group: "another-service", Selector: "v1", Keys: []string{"A"}, Subjects: []string{"foreign"}}.Normalize()
	require.NoError(t, sc.MQManager.ProvisionOwnership(context.Background(), foreign, 0))
	require.NoError(t, sc.MQManager.BindOwnership(context.Background(), foreign))
	wrong, err := sc.SubscribeOwnedEvents(event.OwnedSubscription{Manifest: foreign, NoDueTasks: true, Key: func(*event.Envelope) (string, error) { return "A", nil }, Activate: func(context.Context, ownership.Credential) error { return nil }, Handler: func(context.Context, ownership.Credential, []*event.Envelope) error { return nil }})
	if wrong != nil {
		_ = wrong.Close(context.Background())
	}
	require.ErrorIs(t, err, ownership.ErrUnknown, "ServiceContext不能绕过逻辑服务桥订阅其他服务的业务组")
	for _, subject := range []string{"one", "two"} {
		m := ownership.Manifest{Scope: name + subject, Service: name, Group: name, Selector: "v1", Keys: []string{"A"}, Subjects: []string{subject}}.Normalize()
		require.NoError(t, sc.MQManager.ProvisionOwnership(context.Background(), m, 0))
		require.NoError(t, sc.MQManager.BindOwnership(context.Background(), m))
		_, err := sc.SubscribeOwnedEvents(event.OwnedSubscription{Manifest: m, MaxOwned: 1, NoDueTasks: true, Key: func(*event.Envelope) (string, error) { return "A", nil }, Activate: func(context.Context, ownership.Credential) error { return nil }, Handler: func(context.Context, ownership.Credential, []*event.Envelope) error { return nil }})
		require.NoError(t, err)
	}
	require.Eventually(t, func() bool {
		families, err := prometheus.DefaultGatherer.Gather()
		require.NoError(t, err)
		for _, f := range families {
			if f.GetName() != "core_component_gauge" {
				continue
			}
			for _, metric := range f.Metric {
				labels := map[string]string{}
				for _, label := range metric.Label {
					labels[label.GetName()] = label.GetValue()
				}
				if labels["service"] == name && labels["component"] == "ownership" && labels["name"] == "ownership_ready" {
					return metric.GetGauge().GetValue() == 2
				}
			}
		}
		return false
	}, 2*time.Second, 20*time.Millisecond, "两个范围的Ready应汇总为2")
}
