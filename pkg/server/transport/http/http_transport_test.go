package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coretypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

type limitedHTTPResponse struct{ limit int64 }

func (l limitedHTTPResponse) MaxResponseBytes() int64 { return l.limit }

func TestHTTPTransportHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	transport := New()
	_, err := transport.Send(ctx, &coretypes.PayLoad{Instance: map[string]bool{"forMenu": true}}, "127.0.0.1:1")
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled))

	err = transport.Health(ctx, "127.0.0.1:1")
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled))
}

func TestHTTPTransportRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 1025)))
	}))
	defer server.Close()
	_, err := New().Send(context.Background(), &coretypes.PayLoad{Instance: limitedHTTPResponse{limit: 1024}}, server.URL)
	require.ErrorContains(t, err, "response exceeds")
}
