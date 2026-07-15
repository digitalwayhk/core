package benchmetrics

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type metricRecorder map[string]float64

func (r metricRecorder) ReportMetric(value float64, unit string) { r[unit] = value }

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

func TestReportSuppressesPercentilesWhenWindowSamplesAreInsufficient(t *testing.T) {
	recorder := metricRecorder{}
	Report(recorder, Summarize([]float64{100, 110}, 210, 0))

	require.Equal(t, float64(2), recorder["win-windows"])
	require.NotContains(t, recorder, "win-p50/s")
	require.Contains(t, recorder, "errors")
}

func TestReportIncludesPercentilesAfterMinimumWindowSamples(t *testing.T) {
	samples := make([]float64, MinimumDistributionWindows)
	for index := range samples {
		samples[index] = float64(index + 1)
	}
	recorder := metricRecorder{}
	Report(recorder, Summarize(samples, uint64(len(samples)), 0))

	require.Equal(t, float64(MinimumDistributionWindows), recorder["win-windows"])
	require.Contains(t, recorder, "win-p50/s")
	require.Contains(t, recorder, "win-cv-pct")
}

func TestRotatingSlotKeepsOneWorkloadCycleOnTheSameUser(t *testing.T) {
	require.Equal(t, 0, RotatingSlot(0, 10, 3))
	require.Equal(t, 0, RotatingSlot(9, 10, 3))
	require.Equal(t, 1, RotatingSlot(10, 10, 3))
	require.Equal(t, 2, RotatingSlot(20, 10, 3))
	require.Equal(t, 0, RotatingSlot(30, 10, 3))
}
