package nosql

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultConfigsFailClosedOnCorruption(t *testing.T) {
	production := DefaultProductionConfig(t.TempDir())
	fast := DefaultFastConfig(t.TempDir())

	require.Equal(t, CorruptionPolicyFail, production.CorruptionPolicy)
	require.Equal(t, CorruptionPolicyFail, fast.CorruptionPolicy)
	require.False(t, shouldResetCorruptedCache(production))
	require.False(t, shouldResetCorruptedCache(fast))
}

func TestExplicitCachePolicyAllowsCorruptionReset(t *testing.T) {
	config := DefaultProductionConfig(t.TempDir())
	config.CorruptionPolicy = CorruptionPolicyResetCache

	require.NoError(t, config.Validate())
	require.True(t, shouldResetCorruptedCache(config))
}

func TestBadgerConfigRejectsUnknownCorruptionPolicy(t *testing.T) {
	config := DefaultProductionConfig(t.TempDir())
	config.CorruptionPolicy = CorruptionPolicy("erase_everything")

	err := config.Validate()
	require.Error(t, err)
	require.Contains(t, err.Error(), "corruption_policy")
}
