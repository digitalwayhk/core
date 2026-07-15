package benchmetrics

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSummarizeThroughputWindows(t *testing.T) {
	stats := Summarize([]float64{100, 80, 120, 90, 110}, 490, 10)
	require.Equal(t, 5, stats.Windows)
	require.Equal(t, float64(80), stats.P01)
	require.Equal(t, float64(80), stats.P05)
	require.Equal(t, float64(100), stats.P50)
	require.Equal(t, float64(110), stats.P95)
	require.Equal(t, float64(110), stats.P99)
	require.Equal(t, float64(100), stats.Mean)
	require.Equal(t, float64(2), stats.ErrorPercent)
	require.Greater(t, stats.CVPercent, float64(0))
}
