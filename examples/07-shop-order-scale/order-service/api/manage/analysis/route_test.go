package analysis

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestDashboardRouterInfoPath(t *testing.T) {
	info := (&Dashboard{}).RouterInfo()
	require.Equal(t, "/api/manage/shop-order/analysis", info.GetPath())
	require.Equal(t, types.ManageType, info.GetPathType())
	require.True(t, info.GetAuth())
	require.Equal(t, "POST", info.GetMethod())
}
