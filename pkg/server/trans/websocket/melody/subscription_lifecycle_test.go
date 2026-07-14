package melody

import (
	"sync/atomic"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

var subscriptionFactoryCalls atomic.Int32

type subscriptionFactoryRouter struct{}

func (*subscriptionFactoryRouter) Parse(types.IRequest) error             { return nil }
func (*subscriptionFactoryRouter) Validation(types.IRequest) error        { return nil }
func (*subscriptionFactoryRouter) Do(types.IRequest) (interface{}, error) { return nil, nil }
func (*subscriptionFactoryRouter) RouterInfo() *types.RouterInfo          { return nil }
func (*subscriptionFactoryRouter) New(interface{}) types.IRouter {
	subscriptionFactoryCalls.Add(1)
	return &subscriptionLifecycleRouter{}
}

type subscriptionLifecycleRouter struct {
	Value string
}

func (*subscriptionLifecycleRouter) Parse(types.IRequest) error             { return nil }
func (*subscriptionLifecycleRouter) Validation(types.IRequest) error        { return nil }
func (*subscriptionLifecycleRouter) Do(types.IRequest) (interface{}, error) { return nil, nil }
func (*subscriptionLifecycleRouter) RouterInfo() *types.RouterInfo          { return nil }

func TestParseSubscriptionRequestCreatesDetachedRouter(t *testing.T) {
	subscriptionFactoryCalls.Store(0)
	info := &types.RouterInfo{Path: "/api/test/subscription", ServiceName: "test"}
	info.SetInstance(&subscriptionFactoryRouter{})
	manager := &MelodyManager{}

	subscription, err := manager.parseSubscriptionRequest(info, map[string]interface{}{"Value": "subscription"})
	require.NoError(t, err)
	require.Equal(t, "subscription", subscription.(*subscriptionLifecycleRouter).Value)
	info.ReleaseSubscription(subscription)
	request := info.New()

	require.NotSame(t, subscription, request)
	require.Equal(t, int32(2), subscriptionFactoryCalls.Load())
}
