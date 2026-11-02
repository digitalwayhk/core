package router_test

import (
	"context"
	"fmt"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
	"time"
)

type ownedAdmissionDrain struct{ done <-chan struct{} }

func (r ownedAdmissionDrain) Close(context.Context) error { <-r.done; return nil }

func TestOwnedPublicCallsConcurrentServiceShutdown(t *testing.T) {
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT_RUN: 需要独占CORE_TEST_REDIS_ADDR")
	}
	for _, operation := range []string{"register", "call"} {
		t.Run(operation, func(t *testing.T) {
			name := fmt.Sprintf("owned-shutdown-%s-%d", operation, time.Now().UnixNano())
			cfg := config.NewServiceDefaultConfig(name, 29989)
			cfg.Cluster.Mode = "off"
			cfg.MQ.Mode = "on"
			cfg.MQ.Provider = "redis-stream"
			cfg.MQ.RedisStream = config.RedisStreamMQConfig{Addr: addr, Prefix: name, CommandPoolSize: 4, BlockingReadPoolSize: 4}
			sc := router.NewServiceContextWithConfig(&fakeService{name}, cfg)
			m := ownership.Manifest{Scope: name, Service: name, Group: name, Selector: "v1", Subjects: []string{"input"}, DynamicKeys: &ownership.DynamicKeyPolicy{}}.Normalize()
			require.NoError(t, sc.MQManager.ProvisionOwnership(context.Background(), m, 0))
			require.NoError(t, sc.MQManager.BindOwnership(context.Background(), m))
			require.NoError(t, sc.RegisterOwnedKey(context.Background(), m, "A"))
			store, err := sc.MQManager.OwnershipStore(context.Background())
			require.NoError(t, err)
			credential, err := store.Acquire(context.Background(), ownership.Credential{Scope: name, Key: "A", Service: name, InstanceID: "absent", SessionID: "boot"}, m.Timing.Lease)
			require.NoError(t, err)
			require.NoError(t, store.Ready(context.Background(), credential))
			ready, done := make(chan struct{}), make(chan struct{})
			require.NoError(t, sc.UseResource("admitted-owned-call", ownedAdmissionDrain{done}))
			go func() {
				defer close(done)
				close(ready)
				for i := 0; i < 256; i++ {
					if operation == "register" {
						_ = sc.RegisterOwnedKey(context.Background(), m, "A")
					} else {
						_, _ = sc.CallServiceOwned(context.Background(), &types.PayLoad{TargetService: name, TargetPath: "/api/read"}, name, "A")
					}
				}
			}()
			<-ready
			sc.SetRunState(false)
			require.NoError(t, sc.ShutdownError())
		})
	}
}
