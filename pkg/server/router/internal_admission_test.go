package router

import (
	"context"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/identity"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestInternalDispatchChecksStableAdmission(t *testing.T) {
	for _, inbound := range []bool{false, true} {
		name := "local"
		if inbound {
			name = "grpc-handler"
		}
		t.Run(name, func(t *testing.T) {
			target, route := newLocalInternalCallerTarget(t)
			runtime := &stableIdentityRuntime{external: true, ttl: time.Second}
			runtime.running.Store(true)
			target.stableIdentity = runtime
			defer func() { target.stableIdentity = nil }()
			source := newLocalInternalCallerSource("shop-user")
			payload := &types.PayLoad{TargetService: target.Service.Name, TargetPath: route.info.GetPath(), Instance: &localInternalCallerRoute{}}
			call := func() error {
				if inbound {
					_, err := target.HandleInternalPayload(types.ContextWithTrustedInternalCaller(context.Background(), "shop-user"), payload)
					return err
				}
				_, err := source.CallService(payload)
				return err
			}
			resetLocalInternalCallerCounters()
			require.ErrorIs(t, call(), identity.ErrAdmissionUnavailable)
			require.Zero(t, localInternalCallerCounters.validation.Load())
			require.Zero(t, localInternalCallerCounters.do.Load())
			runtime.observe(time.Now(), nil)
			require.NoError(t, call())
			require.Equal(t, int32(1), localInternalCallerCounters.do.Load())
			runtime.observe(time.Now(), identity.ErrAdmissionUnavailable)
			require.ErrorIs(t, call(), identity.ErrAdmissionUnavailable)
			require.Equal(t, int32(1), localInternalCallerCounters.do.Load())
			source.Service.Name = "shop-supplier"
			_, err := source.CallService(payload)
			require.ErrorIs(t, err, types.ErrInternalCallerForbidden, "内部身份授权必须仍先于准入检查")
		})
	}
}
