package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	consulapi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConsulProviderMetadataRoundTripIncludesTransportPorts(t *testing.T) {
	var mu sync.RWMutex
	var registered *consulapi.AgentServiceRegistration
	var index atomic.Uint64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/agent/service/register":
			var service consulapi.AgentServiceRegistration
			require.NoError(t, json.NewDecoder(r.Body).Decode(&service))
			mu.Lock()
			registered = &service
			mu.Unlock()
			index.Add(1)
			w.WriteHeader(http.StatusOK)
		case strings.HasPrefix(r.URL.Path, "/v1/agent/check/update/"):
			w.WriteHeader(http.StatusOK)
		case strings.HasPrefix(r.URL.Path, "/v1/health/service/orders"):
			mu.RLock()
			service := registered
			mu.RUnlock()
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Consul-Index", strconv.FormatUint(index.Load(), 10))
			if service == nil {
				_, _ = fmt.Fprint(w, "[]")
				return
			}
			entry := []*consulapi.ServiceEntry{{
				Service: &consulapi.AgentService{
					ID: service.ID, Service: service.Name, Address: service.Address,
					Port: service.Port, Meta: service.Meta,
				},
				Checks: consulapi.HealthChecks{{Status: consulapi.HealthPassing}},
			}}
			require.NoError(t, json.NewEncoder(w).Encode(entry))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	provider, err := NewConsulProvider(server.URL)
	require.NoError(t, err)
	node := &NodeInfo{
		ID: "orders-node", ServiceName: "orders", Address: "orders.internal",
		Port: 8080, GRPCPort: 19090, SocketPort: 18080,
		DataCenterID: 2, MachineID: 7, Weight: 3,
	}
	require.NoError(t, provider.Register(context.Background(), node))

	listed, err := provider.List(context.Background(), "orders", NodeStatusRunning)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assertConsulTransportMetadata(t, listed[0])
	got, err := provider.Get(context.Background(), node.ID)
	require.NoError(t, err)
	assertConsulTransportMetadata(t, got)

	watchCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates := make(chan []*NodeInfo, 1)
	stopWatch, err := provider.Watch(watchCtx, "orders", func(nodes []*NodeInfo) {
		select {
		case updates <- nodes:
		default:
		}
	})
	require.NoError(t, err)
	defer stopWatch()
	select {
	case nodes := <-updates:
		require.Len(t, nodes, 1)
		assertConsulTransportMetadata(t, nodes[0])
	case <-time.After(time.Second):
		t.Fatal("Consul Watch 未返回带传输端口的节点")
	}
}

func assertConsulTransportMetadata(t *testing.T, node *NodeInfo) {
	t.Helper()
	assert.Equal(t, 19090, node.GRPCPort)
	assert.Equal(t, 18080, node.SocketPort)
	assert.Equal(t, int64(2), node.DataCenterID)
	assert.Equal(t, int64(7), node.MachineID)
	assert.Equal(t, 3, node.Weight)
}

func TestConsulProviderRegisterCleansRemoteEntryWhenTTLUpdateFails(t *testing.T) {
	var registered atomic.Bool
	var deregistered atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v1/health/service/"):
			_, _ = fmt.Fprint(w, "[]")
		case r.URL.Path == "/v1/agent/service/register":
			registered.Store(true)
			w.WriteHeader(http.StatusOK)
		case strings.HasPrefix(r.URL.Path, "/v1/agent/check/update/"):
			http.Error(w, "ttl failed", http.StatusInternalServerError)
		case r.URL.Path == "/v1/agent/service/deregister/orders-node":
			deregistered.Store(true)
			registered.Store(false)
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	provider, err := NewConsulProvider(server.URL)
	require.NoError(t, err)

	err = provider.Register(context.Background(), &NodeInfo{ID: "node", ServiceName: "orders"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ttl")
	assert.True(t, deregistered.Load(), "TTL 更新失败后必须补偿注销远端服务")
	assert.False(t, registered.Load(), "TTL 更新失败后不得留下远端服务记录")
	_, cached := provider.nodeServices.Load("node")
	assert.False(t, cached, "TTL 更新失败后必须删除本地 nodeServices 映射")
}

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
