package observability_test

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/server/observability"
	"github.com/stretchr/testify/require"
)

func TestProcessLabelsRegisteredOnce(t *testing.T) {
	observability.ResetProcessLabelsForTest()
	t.Cleanup(observability.ResetProcessLabelsForTest)

	require.NoError(t, observability.RegisterProcessLabels("shop-order", "shop-order-dc1-m2"))
	require.NoError(t, observability.RegisterProcessLabels("shop-order", "shop-order-dc1-m2"))

	err := observability.RegisterProcessLabels("shop-order", "other")
	require.ErrorIs(t, err, observability.ErrProcessLabelsConflict)

	svc, id, ok := observability.ProcessLabels()
	require.True(t, ok)
	require.Equal(t, "shop-order", svc)
	require.Equal(t, "shop-order-dc1-m2", id)
}

func TestProcessLabelsRejectEmpty(t *testing.T) {
	observability.ResetProcessLabelsForTest()
	t.Cleanup(observability.ResetProcessLabelsForTest)
	require.Error(t, observability.RegisterProcessLabels("", "id"))
	require.Error(t, observability.RegisterProcessLabels("svc", ""))
}
