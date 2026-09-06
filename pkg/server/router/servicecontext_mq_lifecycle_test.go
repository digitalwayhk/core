package router

import (
	"context"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/stretchr/testify/require"
)

type serviceContextLifecycleProvider struct {
	ensured mq.LifecyclePolicy
}

func (*serviceContextLifecycleProvider) Name() string                  { return "servicecontext-lifecycle" }
func (*serviceContextLifecycleProvider) Connect(context.Context) error { return nil }
func (*serviceContextLifecycleProvider) Close() error                  { return nil }
func (*serviceContextLifecycleProvider) Publish(context.Context, string, []byte, *mq.PublishOptions) error {
	return nil
}
func (*serviceContextLifecycleProvider) Subscribe(context.Context, string, func(*mq.Message)) (func(), error) {
	return func() {}, nil
}
func (*serviceContextLifecycleProvider) Health(context.Context) error { return nil }
func (*serviceContextLifecycleProvider) LifecycleCapabilities() mq.LifecycleCapabilities {
	return mq.LifecycleCapabilities{
		DurablePublishAck: true, RequiredGroups: true, SafeReclaim: true,
		RetainedMessages: true, RetainedBytes: true,
	}
}
func (p *serviceContextLifecycleProvider) EnsureLifecycle(_ context.Context, policy mq.LifecyclePolicy) error {
	p.ensured = policy
	return nil
}
func (*serviceContextLifecycleProvider) InspectLifecycle(_ context.Context, policy mq.LifecyclePolicy) (mq.LifecycleSnapshot, error) {
	return mq.LifecycleSnapshot{
		Subject: policy.Subject, PolicyFingerprint: policy.Fingerprint(),
		ObservedAt: time.Now(), State: mq.LifecycleStateOK,
	}, nil
}
func (*serviceContextLifecycleProvider) ReclaimLifecycle(context.Context, mq.LifecyclePolicy, mq.LifecycleSnapshot) (mq.ReclaimResult, error) {
	return mq.ReclaimResult{}, nil
}

// TestServiceContextRequireMessageLifecycleDelegatesToManager 验证应用只需在组合根声明策略。
func TestServiceContextRequireMessageLifecycleDelegatesToManager(t *testing.T) {
	provider := &serviceContextLifecycleProvider{}
	manager := mq.NewManager()
	manager.Register(provider)
	require.NoError(t, manager.SetCurrent(provider.Name()))
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	sc := &ServiceContext{MQManager: manager}
	policy := mq.LifecyclePolicy{
		Subject: "fills", Mode: mq.LifecycleModeObserve,
		RequiredGroups: []mq.ConsumerGroupRequirement{{Name: "positions", Start: mq.StartFromAllRetained}},
	}

	require.NoError(t, sc.RequireMessageLifecycle(context.Background(), policy))
	require.Equal(t, policy.Normalize().Fingerprint(), provider.ensured.Fingerprint())
}

// TestServiceContextRequireMessageLifecycleFailsWithoutMQ 验证必需生命周期不会在 MQ 缺失时静默降级。
func TestServiceContextRequireMessageLifecycleFailsWithoutMQ(t *testing.T) {
	sc := &ServiceContext{}
	err := sc.RequireMessageLifecycle(context.Background(), mq.LifecyclePolicy{})
	require.ErrorIs(t, err, mq.ErrNotConnected)
}
