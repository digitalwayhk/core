package mq

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestLifecyclePolicyValidateRejectsUnsafeOrAmbiguousPolicy 验证策略缺少必需信息或含冲突组时 fail closed。
func TestLifecyclePolicyValidateRejectsUnsafeOrAmbiguousPolicy(t *testing.T) {
	tests := []struct {
		name   string
		policy LifecyclePolicy
	}{
		{name: "empty subject", policy: LifecyclePolicy{}},
		{name: "no required groups", policy: LifecyclePolicy{Subject: "fills", Mode: LifecycleModeEnforce}},
		{name: "duplicate group", policy: LifecyclePolicy{
			Subject: "fills", Mode: LifecycleModeEnforce,
			RequiredGroups: []ConsumerGroupRequirement{
				{Name: "positions", Start: StartFromAllRetained},
				{Name: "positions", Start: StartFromNew},
			},
		}},
		{name: "invalid start", policy: LifecyclePolicy{
			Subject: "fills", Mode: LifecycleModeEnforce,
			RequiredGroups: []ConsumerGroupRequirement{{Name: "positions", Start: "latest"}},
		}},
		{name: "negative batch", policy: LifecyclePolicy{
			Subject: "fills", Mode: LifecycleModeEnforce,
			RequiredGroups: []ConsumerGroupRequirement{{Name: "positions", Start: StartFromAllRetained}},
			Reclaim:        ReclaimBudget{BatchSize: -1},
		}},
		{name: "retry without dlq", policy: LifecyclePolicy{
			Subject: "fills", Mode: LifecycleModeEnforce,
			RequiredGroups: []ConsumerGroupRequirement{{Name: "positions", Start: StartFromAllRetained}},
			Retry:          RetryPolicy{MaxDeliveries: 3},
		}},
		{name: "soft above hard", policy: LifecyclePolicy{
			Subject: "fills", Mode: LifecycleModeEnforce,
			RequiredGroups: []ConsumerGroupRequirement{{Name: "positions", Start: StartFromAllRetained}},
			Capacity:       CapacityPolicy{SoftMessages: 11, HardMessages: 10},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, tt.policy.Validate())
		})
	}
}

// TestLifecyclePolicyNormalizeAppliesConservativeDefaults 验证零值不开启删除，且回收 worker 始终有界。
func TestLifecyclePolicyNormalizeAppliesConservativeDefaults(t *testing.T) {
	policy := LifecyclePolicy{
		Subject:        "fills",
		RequiredGroups: []ConsumerGroupRequirement{{Name: "positions", Start: StartFromAllRetained}},
	}.Normalize()

	require.Equal(t, LifecycleModeObserve, policy.Mode)
	require.Equal(t, 30*time.Second, policy.Reclaim.Interval)
	require.Equal(t, 1000, policy.Reclaim.BatchSize)
	require.Equal(t, 500*time.Millisecond, policy.Reclaim.TimeBudget)
	require.NoError(t, policy.Validate())
}

// TestLifecyclePolicyFingerprintIgnoresGroupDeclarationOrder 验证等价 manifest 不因 slice 顺序产生冲突。
func TestLifecyclePolicyFingerprintIgnoresGroupDeclarationOrder(t *testing.T) {
	first := validLifecyclePolicy("users", "positions")
	second := validLifecyclePolicy("positions", "users")

	require.Equal(t, first.Fingerprint(), second.Fingerprint())
}

// TestLifecyclePolicyFingerprintChangesForSafetyRelevantFields 验证保留和组起点变更会切换策略代际。
func TestLifecyclePolicyFingerprintChangesForSafetyRelevantFields(t *testing.T) {
	base := validLifecyclePolicy("positions")
	changedRetention := base
	changedRetention.Retention.MinAge++
	changedStart := base
	changedStart.RequiredGroups = append([]ConsumerGroupRequirement(nil), base.RequiredGroups...)
	changedStart.RequiredGroups[0].Start = StartFromNew

	require.NotEqual(t, base.Fingerprint(), changedRetention.Fingerprint())
	require.NotEqual(t, base.Fingerprint(), changedStart.Fingerprint())
}

func validLifecyclePolicy(groups ...string) LifecyclePolicy {
	required := make([]ConsumerGroupRequirement, 0, len(groups))
	for _, group := range groups {
		required = append(required, ConsumerGroupRequirement{Name: group, Start: StartFromAllRetained})
	}
	return LifecyclePolicy{
		Subject:        "fills",
		Mode:           LifecycleModeEnforce,
		RequiredGroups: required,
		Retention:      RetentionPolicy{MinAge: time.Hour},
		Reclaim: ReclaimBudget{
			Interval: 30 * time.Second, BatchSize: 1000, TimeBudget: 500 * time.Millisecond,
		},
	}
}
