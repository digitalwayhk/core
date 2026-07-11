package manage_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/api/manage"
	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestSyncSubscriptionUsesServiceScopedBroker(t *testing.T) {
	serviceName := fmt.Sprintf("manage-notice-relay-%d", time.Now().UnixNano())
	serviceConfig := config.NewServiceDefaultConfig(serviceName, 32201)
	serviceConfig.Cluster.Mode = "off"
	serviceConfig.MQ.Mode = "off"
	serviceConfig.Transport.Internal = ""
	serviceConfig.Transport.Fallback = nil
	require.NotNil(t, router.NewServiceContextWithConfig(
		&fakeManageSvc{name: serviceName},
		serviceConfig,
	))

	provider := cluster.NewLocalProvider(time.Hour, time.Hour, time.Hour)
	provider.Start()
	t.Cleanup(func() { require.NoError(t, provider.Close()) })
	require.NoError(t, provider.Register(context.Background(), &cluster.NodeInfo{
		ID:          "peer-node",
		ServiceName: serviceName,
		Address:     "127.0.0.1",
		Port:        18080,
	}))

	scopedBroker := cluster.NewCrossNodeNoticeBroker(provider, serviceName, "local-node")
	legacyBroker := cluster.NewCrossNodeNoticeBroker(provider, serviceName, "legacy-node")
	types.SetCrossNodeForwarderForService(serviceName, scopedBroker)
	types.SetCrossNodeForwarder(legacyBroker)
	t.Cleanup(func() {
		types.ClearCrossNodeForwarderForService(serviceName, scopedBroker)
		types.SetCrossNodeForwarder(nil)
	})

	forwarded := make(chan string, 1)
	scopedBroker.SetSender(func(_ context.Context, _ string, _ []byte, path string) ([]byte, error) {
		forwarded <- path
		return nil, nil
	})

	api := &manage.SyncSubscription{
		RoutePath: "/ws/orders",
		Hash:      88,
		NodeID:    "peer-node",
		Active:    true,
	}
	_, err := api.Do(&mockRequest{serviceName: serviceName})
	require.NoError(t, err)

	scopedBroker.ForwardNotice(context.Background(), api.RoutePath, api.Hash, "order")
	select {
	case path := <-forwarded:
		require.Equal(t, "/api/servermanage/ws/notice", path)
	case <-time.After(2 * time.Second):
		t.Fatal("服务作用域 broker 未收到订阅同步")
	}
}
