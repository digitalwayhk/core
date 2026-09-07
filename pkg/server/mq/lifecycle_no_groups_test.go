package mq

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestLifecycleExplicitNoRequiredGroups 验证显式无组与漏配不同，且必须声明正数保留期。
func TestLifecycleExplicitNoRequiredGroups(t *testing.T) {
	var policy LifecyclePolicy
	require.NoError(t, json.Unmarshal([]byte(`{"Subject":"history","NoRequiredGroups":true,"Retention":{"MinAge":3600000000000}}`), &policy))
	require.NoError(t, policy.Validate())
	missing := LifecyclePolicy{Subject: "history", Retention: RetentionPolicy{MinAge: time.Hour}}
	require.ErrorIs(t, missing.Validate(), ErrLifecyclePolicyInvalid)
	require.NotEqual(t, missing.Fingerprint(), policy.Fingerprint())
	policy.RequiredGroups = []ConsumerGroupRequirement{{Name: "reader", Start: StartFromAllRetained}}
	require.ErrorIs(t, policy.Validate(), ErrLifecyclePolicyInvalid)
	policy.RequiredGroups = nil
	policy.Retention.MinAge = 0
	require.ErrorIs(t, policy.Validate(), ErrLifecyclePolicyInvalid)
	policy.Retention.MinAge = time.Hour
	policy.Retry.HandlerTimeout = time.Second
	require.ErrorIs(t, policy.Validate(), ErrLifecyclePolicyInvalid)
}

// TestLifecycleNoRequiredGroupsCapabilitiesAndSubscription 验证能力独立声明及订阅门禁。
func TestLifecycleNoRequiredGroupsCapabilitiesAndSubscription(t *testing.T) {
	policy := LifecyclePolicy{Subject: "history", NoRequiredGroups: true, Retention: RetentionPolicy{MinAge: time.Hour}}.Normalize()
	caps := allLifecycleCapabilities()
	require.ErrorIs(t, validateLifecycleCapabilities(caps, policy), ErrLifecycleUnsupported)
	caps.RequiredGroups = false
	caps.NoRequiredGroups = true
	require.NoError(t, validateLifecycleCapabilities(caps, policy))
	caps.RequiredGroups = true
	manager := managerWithLifecycleTestProvider(t, &lifecycleTestProvider{capabilities: caps})
	require.NoError(t, manager.RequireMessageLifecycle(context.Background(), policy))
	_, err := manager.SubscribeReliable(context.Background(), policy.Subject, ReliableSubscribeOptions{Group: "reader"}, func(*Message) error { return nil })
	require.ErrorIs(t, err, ErrLifecycleRequiredGroupMismatch)
	// 当前普通 Subscribe 也会创建 Broker 消费组，不能绕过无组声明。
	_, err = manager.Subscribe(context.Background(), policy.Subject, func(*Message) {})
	require.ErrorIs(t, err, ErrLifecycleRequiredGroupMismatch)
	withGroup := policy
	withGroup.NoRequiredGroups = false
	withGroup.RequiredGroups = []ConsumerGroupRequirement{{Name: "reader", Start: StartFromAllRetained}}
	require.ErrorIs(t, manager.RequireMessageLifecycle(context.Background(), withGroup), ErrLifecyclePolicyConflict)
}

// TestLifecycleNoRequiredGroupsFingerprintCompatibility 验证零值字段不改变旧 manifest JSON。
func TestLifecycleNoRequiredGroupsFingerprintCompatibility(t *testing.T) {
	data, err := json.Marshal(validLifecyclePolicy("reader").Normalize())
	require.NoError(t, err)
	require.NotContains(t, string(data), "NoRequiredGroups")
}
