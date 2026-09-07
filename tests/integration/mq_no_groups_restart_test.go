//go:build integration

// 本文件补充共享 Broker 重启场景中的纯保留主题，复用外层真实重启编排。
package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/stretchr/testify/require"
)

func noGroupRestartPolicy() mq.LifecyclePolicy {
	return mq.LifecyclePolicy{
		Subject: restartSubject + ".no-groups", Mode: mq.LifecycleModeObserve,
		NoRequiredGroups: true, Retention: mq.RetentionPolicy{MinAge: 50 * time.Millisecond},
		Reclaim: mq.ReclaimBudget{Interval: time.Hour, BatchSize: 2, TimeBudget: time.Second},
	}
}

func prepareNoGroupRestart(t *testing.T, ctx context.Context, provider mq.LifecycleMQProvider) {
	t.Helper()
	policy := noGroupRestartPolicy()
	require.NoError(t, provider.EnsureLifecycle(ctx, policy))
	for i := 0; i < 3; i++ {
		require.NoError(t, provider.Publish(ctx, policy.Subject, []byte("retained-before-restart"), nil))
	}
	snapshot, err := provider.InspectLifecycle(ctx, policy)
	require.NoError(t, err)
	require.NotNil(t, snapshot.RetainedMessages)
	require.EqualValues(t, 3, *snapshot.RetainedMessages)
}

func recoverNoGroupRestart(t *testing.T, ctx context.Context, provider mq.LifecycleMQProvider) {
	t.Helper()
	policy := noGroupRestartPolicy()
	require.NoError(t, provider.EnsureLifecycle(ctx, policy))
	snapshot, err := provider.InspectLifecycle(ctx, policy)
	require.NoError(t, err)
	require.NotNil(t, snapshot.RetainedMessages)
	require.EqualValues(t, 3, *snapshot.RetainedMessages)
	require.Empty(t, snapshot.Groups)
	result, err := provider.ReclaimLifecycle(ctx, policy, snapshot)
	require.NoError(t, err)
	require.Zero(t, result.Reclaimed)
	policy.Mode = mq.LifecycleModeEnforce
	require.NoError(t, provider.EnsureLifecycle(ctx, policy))
	for _, expected := range []int64{2, 1} {
		snapshot, err = provider.InspectLifecycle(ctx, policy)
		require.NoError(t, err)
		result, err = provider.ReclaimLifecycle(ctx, policy, snapshot)
		require.NoError(t, err)
		require.Equal(t, expected, result.Reclaimed)
	}
	snapshot, err = provider.InspectLifecycle(ctx, policy)
	require.NoError(t, err)
	require.Zero(t, *snapshot.RetainedMessages)
}
