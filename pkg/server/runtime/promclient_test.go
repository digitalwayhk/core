package runtime_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/runtime"
	"github.com/stretchr/testify/require"
)

func TestPromClientQuerySuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/query", r.URL.Path)
		require.NotEmpty(t, r.URL.Query().Get("query"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1710000000,"12.5"]}]}}`))
	}))
	t.Cleanup(srv.Close)

	client := runtime.NewPromClient(srv.URL, time.Second)
	vec, err := client.Query(context.Background(), `up`, time.Now())
	require.NoError(t, err)
	require.Len(t, vec, 1)
	require.Equal(t, 12.5, vec[0].Value)
}

func TestPromClientQueryUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	client := runtime.NewPromClient(srv.URL, time.Second)
	_, err := client.Query(context.Background(), `up`, time.Now())
	require.ErrorIs(t, err, runtime.ErrPrometheusUnavailable)
}
