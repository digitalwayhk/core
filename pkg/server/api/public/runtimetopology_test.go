package public

import (
	"encoding/json"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/runtime"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestRuntimeTopologyRouterInfoIsServerManage(t *testing.T) {
	info := (&RuntimeTopology{}).RouterInfo()
	require.Equal(t, "/api/servermanage/runtimetopology", info.GetPath())
	require.Equal(t, types.ServerManagerType, info.GetPathType())
}

func TestRuntimeServiceRouterInfoIsServerManage(t *testing.T) {
	info := (&RuntimeService{}).RouterInfo()
	require.Equal(t, "/api/servermanage/runtimeservice", info.GetPath())
	require.Equal(t, types.ServerManagerType, info.GetPathType())
}

func TestRuntimeTopologyRejectsBadWindow(t *testing.T) {
	h := &RuntimeTopology{Window: "7d"}
	err := h.Validation(&stubRequest{authorized: true})
	require.Error(t, err)
}

func TestRuntimeTopologyAcceptsWindow(t *testing.T) {
	h := &RuntimeTopology{Window: "15s"}
	err := h.Validation(&stubRequest{authorized: true})
	require.NoError(t, err)
}

func TestRuntimeServiceRequiresService(t *testing.T) {
	h := &RuntimeService{Window: "15s"}
	err := h.Validation(&stubRequest{authorized: true})
	require.Error(t, err)
}

func TestRuntimeTopologyParseJSONBody(t *testing.T) {
	h := &RuntimeTopology{}

	err := h.Parse(&stubRequest{
		authorized: true,
		body:       map[string]string{"window": "1h"},
	})

	require.NoError(t, err)
	require.Equal(t, "1h", h.Window)
}

func TestRuntimeServiceParseJSONBody(t *testing.T) {
	h := &RuntimeService{}

	err := h.Parse(&stubRequest{
		authorized: true,
		body: map[string]string{
			"window":  "5m",
			"service": "shop-user",
		},
	})

	require.NoError(t, err)
	require.Equal(t, "5m", h.Window)
	require.Equal(t, "shop-user", h.Service)
}

func TestRuntimeTopologyDoWithoutAggregator(t *testing.T) {
	h := &RuntimeTopology{Window: "15s"}
	out, err := h.Do(&stubRequest{authorized: true, service: "demo"})
	require.NoError(t, err)
	resp, ok := out.(*runtime.TopologyResponse)
	require.True(t, ok)
	require.Equal(t, runtime.StateNotCollected, resp.Status)
	require.NotEmpty(t, resp.Warnings)
}

type stubRequest struct {
	authorized bool
	service    string
	values     map[string]string
	body       map[string]string
}

func (s *stubRequest) GetTraceId() string { return "" }
func (s *stubRequest) GetUser() (string, string) {
	return "", ""
}
func (s *stubRequest) GetClientIP() string { return "127.0.0.1" }
func (s *stubRequest) NewID() uint         { return 1 }
func (s *stubRequest) Authorized() bool    { return s.authorized }
func (s *stubRequest) CallService(types.IRouter, ...func(types.IResponse)) (types.IResponse, error) {
	return nil, nil
}
func (s *stubRequest) CallTargetService(types.IRouter, *types.TargetInfo, ...func(types.IResponse)) (types.IResponse, error) {
	return nil, nil
}
func (s *stubRequest) GetValue(key string) string {
	if s.values == nil {
		return ""
	}
	return s.values[key]
}
func (s *stubRequest) Bind(target interface{}) error {
	if s.body == nil {
		return nil
	}
	raw, err := json.Marshal(s.body)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}
func (s *stubRequest) GoZeroBind(interface{}) error                   { return nil }
func (s *stubRequest) NewResponse(interface{}, error) types.IResponse { return nil }
func (s *stubRequest) GetPath() string                                { return "" }
func (s *stubRequest) GetClaims(string) interface{}                   { return nil }
func (s *stubRequest) ServiceName() string                            { return s.service }
func (s *stubRequest) GetServerInfo() *types.TargetInfo               { return nil }
func (s *stubRequest) GetTargetServerInfo(string) *types.TargetInfo   { return nil }
