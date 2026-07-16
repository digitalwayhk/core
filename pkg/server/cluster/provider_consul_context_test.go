package cluster

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestConsulProviderOperationsHonorContext(t *testing.T) {
	tests := []struct {
		name       string
		blockPath  string
		healthBody string
		invoke     func(context.Context, *ConsulProvider) error
	}{
		{
			name: "list", blockPath: "/v1/health/service/orders",
			invoke: func(ctx context.Context, provider *ConsulProvider) error {
				_, err := provider.List(ctx, "orders")
				return err
			},
		},
		{
			name: "register", blockPath: "/v1/agent/service/register", healthBody: "[]",
			invoke: func(ctx context.Context, provider *ConsulProvider) error {
				return provider.Register(ctx, &NodeInfo{ID: "node", ServiceName: "orders"})
			},
		},
		{
			name: "deregister", blockPath: "/v1/agent/service/deregister/orders-node", healthBody: consulHealthEntryJSON,
			invoke: func(ctx context.Context, provider *ConsulProvider) error {
				return provider.Deregister(ctx, "node")
			},
		},
		{
			name: "update-ttl", blockPath: "/v1/agent/check/update/service:orders-node", healthBody: consulHealthEntryJSON,
			invoke: func(ctx context.Context, provider *ConsulProvider) error {
				return provider.Heartbeat(ctx, "node")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestEntered := make(chan struct{})
			releaseRequest := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if strings.HasPrefix(request.URL.Path, tt.blockPath) {
					close(requestEntered)
					select {
					case <-request.Context().Done():
					case <-releaseRequest:
					}
					return
				}
				if strings.HasPrefix(request.URL.Path, "/v1/health/service/") {
					writer.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(writer, tt.healthBody)
					return
				}
				http.NotFound(writer, request)
			}))
			defer server.Close()
			defer close(releaseRequest)
			provider, err := NewConsulProvider(server.URL)
			require.NoError(t, err)

			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- tt.invoke(ctx, provider) }()
			select {
			case <-requestEntered:
			case <-time.After(time.Second):
				t.Fatal("Consul 请求未进入阻塞 transport")
			}
			select {
			case err := <-result:
				require.Error(t, err)
				require.True(t, errors.Is(err, context.DeadlineExceeded), "应返回 context 截止错误，得到 %v", err)
			case <-time.After(time.Second):
				t.Fatal("Consul 请求未随 context 截止返回")
			}
		})
	}
}

const consulHealthEntryJSON = `[{"Service":{"ID":"orders-node","Service":"orders","Address":"127.0.0.1","Port":8080,"Meta":{"node_id":"node"}},"Checks":[]}]`
