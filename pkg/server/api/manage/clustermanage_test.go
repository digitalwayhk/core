package manage_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/api/manage"
	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRequest is a minimal types.IRequest implementation for manage API tests.
// It provides the service name and JSON-binds a preset body into target structs.
type mockRequest struct {
	serviceName string
	body        interface{}
}

func (r *mockRequest) ServiceName() string { return r.serviceName }
func (r *mockRequest) Bind(v interface{}) error {
	if r.body == nil {
		return nil
	}
	data, err := json.Marshal(r.body)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// Stubs for the remaining IRequest methods.
func (r *mockRequest) GetTraceId() string                             { return "" }
func (r *mockRequest) GetUser() (string, string)                      { return "", "" }
func (r *mockRequest) GetClientIP() string                            { return "127.0.0.1" }
func (r *mockRequest) NewID() uint                                    { return 0 }
func (r *mockRequest) Authorized() bool                               { return true }
func (r *mockRequest) GetValue(string) string                         { return "" }
func (r *mockRequest) GoZeroBind(interface{}) error                   { return nil }
func (r *mockRequest) NewResponse(interface{}, error) types.IResponse { return nil }
func (r *mockRequest) GetPath() string                                { return "" }
func (r *mockRequest) GetClaims(string) interface{}                   { return nil }
func (r *mockRequest) GetServerInfo() *types.TargetInfo               { return nil }
func (r *mockRequest) GetTargetServerInfo(string) *types.TargetInfo   { return nil }
func (r *mockRequest) CallService(types.IRouter, ...func(types.IResponse)) (types.IResponse, error) {
	return nil, nil
}
func (r *mockRequest) CallTargetService(types.IRouter, *types.TargetInfo, ...func(types.IResponse)) (types.IResponse, error) {
	return nil, nil
}

// fakeManageSvc is a minimal IService used to register a ServiceContext.
type fakeManageSvc struct{ name string }

func (f *fakeManageSvc) ServiceName() string                    { return f.name }
func (f *fakeManageSvc) Routers() []types.IRouter               { return nil }
func (f *fakeManageSvc) SubscribeRouters() []*types.ObserveArgs { return nil }

// mustCreateContext is a test helper that creates (or retrieves the cached)
// ServiceContext for the given service name.
func mustCreateContext(t *testing.T, name string) {
	t.Helper()
	sc := router.NewServiceContext(&fakeManageSvc{name})
	require.NotNil(t, sc)
}

// --- ClusterSwitchProvider tests ---

// TestClusterSwitchProvider_NilServiceContext_ReturnsNotInitialised verifies that
// Do returns a "not initialised" result when no ServiceContext has been registered
// for the requested service name.
func TestClusterSwitchProvider_NilServiceContext_ReturnsNotInitialised(t *testing.T) {
	api := &manage.ClusterSwitchProvider{Action: "complete"}
	req := &mockRequest{serviceName: "manage-test-unregistered-svc"}

	result, err := api.Do(req)
	require.NoError(t, err, "Do should not return an error for unregistered service")
	require.NotNil(t, result)
	status, ok := result.(*manage.ClusterSwitchProvider)
	require.True(t, ok)
	assert.Contains(t, status.Result, "not initialised",
		"result should indicate switcher is not initialised")
}

// TestClusterSwitchProvider_UnknownAction_ReturnsError verifies that an unrecognised
// action string causes Do to return a descriptive error.
func TestClusterSwitchProvider_UnknownAction_ReturnsError(t *testing.T) {
	mustCreateContext(t, "manage-test-unknown-action")

	api := &manage.ClusterSwitchProvider{Action: "teleport"}
	req := &mockRequest{serviceName: "manage-test-unknown-action"}

	_, err := api.Do(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown action", "error should name the unknown action")
}

// TestClusterSwitchProvider_CompleteBeforeBegin_ReturnsError verifies that calling
// Complete when no migration is in progress returns an error.
func TestClusterSwitchProvider_CompleteBeforeBegin_ReturnsError(t *testing.T) {
	mustCreateContext(t, "manage-test-complete-no-begin")

	api := &manage.ClusterSwitchProvider{Action: "complete"}
	req := &mockRequest{serviceName: "manage-test-complete-no-begin"}

	_, err := api.Do(req)
	require.Error(t, err, "complete without a prior begin should return an error")
}

// TestClusterSwitchProvider_RollbackBeforeBegin_ReturnsError verifies that calling
// Rollback when no migration is in progress returns an error.
func TestClusterSwitchProvider_RollbackBeforeBegin_ReturnsError(t *testing.T) {
	mustCreateContext(t, "manage-test-rollback-no-begin")

	api := &manage.ClusterSwitchProvider{Action: "rollback"}
	req := &mockRequest{serviceName: "manage-test-rollback-no-begin"}

	_, err := api.Do(req)
	require.Error(t, err, "rollback without a prior begin should return an error")
}

// TestClusterSwitchProvider_UnsupportedProvider_ReturnsError verifies that
// requesting a begin with an unsupported target provider returns an error.
func TestClusterSwitchProvider_UnsupportedProvider_ReturnsError(t *testing.T) {
	mustCreateContext(t, "manage-test-unsupported-prov")

	api := &manage.ClusterSwitchProvider{
		Action:         "begin",
		TargetProvider: "fakedb",
		Endpoints:      []string{"localhost:1234"},
	}
	req := &mockRequest{serviceName: "manage-test-unsupported-prov"}

	_, err := api.Do(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported target provider")
}

// --- ClusterStatus tests ---

// TestClusterStatus_NoContext_ReturnsNoneProvider verifies that ClusterStatus.Do
// returns a "none" provider name when no ServiceContext exists for the service.
func TestClusterStatus_NoContext_ReturnsNoneProvider(t *testing.T) {
	api := &manage.ClusterStatus{}
	req := &mockRequest{serviceName: "manage-test-status-nocontext"}

	result, err := api.Do(req)
	require.NoError(t, err)
	require.NotNil(t, result)
	status, ok := result.(*manage.ClusterStatus)
	require.True(t, ok)
	assert.Equal(t, "none", status.ProviderName)
}

// TestClusterStatus_WithContext_ReturnsProviderName verifies that ClusterStatus.Do
// returns the correct provider name for a registered service.
func TestClusterStatus_WithContext_ReturnsProviderName(t *testing.T) {
	mustCreateContext(t, "manage-test-status-with-ctx")

	api := &manage.ClusterStatus{}
	req := &mockRequest{serviceName: "manage-test-status-with-ctx"}

	result, err := api.Do(req)
	require.NoError(t, err)
	require.NotNil(t, result)
	status, ok := result.(*manage.ClusterStatus)
	require.True(t, ok)
	assert.NotEmpty(t, status.ProviderName, "provider name should be set for a registered service")
	assert.NotEqual(t, "none", status.ProviderName)
}

// --- ClusterNodes tests ---

// TestClusterNodes_NoContext_ReturnsEmptyList verifies that ClusterNodes.Do
// returns an empty node list when no ServiceContext exists.
func TestClusterNodes_NoContext_ReturnsEmptyList(t *testing.T) {
	api := &manage.ClusterNodes{}
	req := &mockRequest{serviceName: "manage-test-nodes-nocontext"}

	result, err := api.Do(req)
	require.NoError(t, err)
	require.NotNil(t, result)
	nodes, ok := result.(*manage.ClusterNodes)
	require.True(t, ok)
	assert.Empty(t, nodes.Nodes)
}

// --- ClusterSwitchProvider success path tests ---

// TestClusterSwitchProvider_PreseededBegin_CompleteSucceeds verifies that after
// manually calling ClusterSwitcher.Begin (bypassing buildTargetProvider), the
// API complete action promotes the pending provider and updates ClusterProvider.
func TestClusterSwitchProvider_PreseededBegin_CompleteSucceeds(t *testing.T) {
	const svcName = "manage-test-begin-complete"
	sc := router.NewServiceContext(&fakeManageSvc{svcName})
	require.NotNil(t, sc)

	newProvider := cluster.NewLocalProvider(10*time.Second, 10*time.Second, 30*time.Second)
	newProvider.Start()
	t.Cleanup(func() { newProvider.Close() })

	err := sc.ClusterSwitcher.Begin(context.Background(), newProvider)
	require.NoError(t, err, "direct Begin should succeed")

	api := &manage.ClusterSwitchProvider{Action: "complete"}
	req := &mockRequest{serviceName: svcName}
	result, err := api.Do(req)
	require.NoError(t, err)
	require.NotNil(t, result)
	status, ok := result.(*manage.ClusterSwitchProvider)
	require.True(t, ok)
	assert.Equal(t, "ok", status.Result)

	// SyncProviderAfterSwitch is called inside Do("complete"); ClusterProvider
	// should now point to the newly promoted provider.
	assert.Same(t, newProvider, sc.ClusterProvider,
		"ClusterProvider should be updated to newProvider after complete")
}

// TestClusterSwitchProvider_PreseededBegin_RollbackSucceeds verifies that after
// manually calling ClusterSwitcher.Begin, the API rollback action aborts the
// migration and leaves ClusterProvider unchanged.
func TestClusterSwitchProvider_PreseededBegin_RollbackSucceeds(t *testing.T) {
	const svcName = "manage-test-begin-rollback"
	sc := router.NewServiceContext(&fakeManageSvc{svcName})
	require.NotNil(t, sc)

	originalProvider := sc.ClusterProvider

	newProvider := cluster.NewLocalProvider(10*time.Second, 10*time.Second, 30*time.Second)
	newProvider.Start()
	t.Cleanup(func() { newProvider.Close() })

	err := sc.ClusterSwitcher.Begin(context.Background(), newProvider)
	require.NoError(t, err)

	api := &manage.ClusterSwitchProvider{Action: "rollback"}
	req := &mockRequest{serviceName: svcName}
	result, err := api.Do(req)
	require.NoError(t, err)
	require.NotNil(t, result)
	status, ok := result.(*manage.ClusterSwitchProvider)
	require.True(t, ok)
	assert.Equal(t, "ok", status.Result)

	// After rollback the original provider should still be active.
	assert.Same(t, originalProvider, sc.ClusterProvider,
		"ClusterProvider should remain unchanged after rollback")
	assert.NotSame(t, newProvider, sc.ClusterProvider)
}

// TestClusterStatus_AfterProviderSwitch_ReturnsNewProvider verifies that
// ClusterStatus reflects the new provider after a successful switch.
func TestClusterStatus_AfterProviderSwitch_ReturnsNewProvider(t *testing.T) {
	const svcName = "manage-test-status-after-switch"
	sc := router.NewServiceContext(&fakeManageSvc{svcName})
	require.NotNil(t, sc)

	newProvider := cluster.NewLocalProvider(10*time.Second, 10*time.Second, 30*time.Second)
	newProvider.Start()
	t.Cleanup(func() { newProvider.Close() })

	require.NoError(t, sc.ClusterSwitcher.Begin(context.Background(), newProvider))

	completeAPI := &manage.ClusterSwitchProvider{Action: "complete"}
	_, err := completeAPI.Do(&mockRequest{serviceName: svcName})
	require.NoError(t, err)

	// ClusterStatus should now reflect the updated provider.
	statusAPI := &manage.ClusterStatus{}
	result, err := statusAPI.Do(&mockRequest{serviceName: svcName})
	require.NoError(t, err)
	require.NotNil(t, result)
	status, ok := result.(*manage.ClusterStatus)
	require.True(t, ok)
	assert.Equal(t, newProvider.Name(), status.ProviderName,
		"ClusterStatus should report the new provider after a successful switch")
}

// --- Full API begin→complete/rollback tests using TargetProvider="local" ---

// TestClusterSwitchProvider_BeginLocal_Succeeds verifies that the begin action
// with TargetProvider="local" (newly supported) returns ok without error.
func TestClusterSwitchProvider_BeginLocal_Succeeds(t *testing.T) {
	const svcName = "manage-test-api-begin-local"
	mustCreateContext(t, svcName)

	api := &manage.ClusterSwitchProvider{
		Action:         "begin",
		TargetProvider: "local",
	}
	req := &mockRequest{serviceName: svcName}

	result, err := api.Do(req)
	require.NoError(t, err)
	require.NotNil(t, result)
	status, ok := result.(*manage.ClusterSwitchProvider)
	require.True(t, ok)
	assert.Equal(t, "ok", status.Result, "begin with local provider should succeed")
}

// TestClusterSwitchProvider_APIBeginThenComplete_Succeeds verifies the full
// begin→complete flow when both actions go through the ClusterSwitchProvider
// API (i.e. begin uses buildTargetProvider("local", …)).
func TestClusterSwitchProvider_APIBeginThenComplete_Succeeds(t *testing.T) {
	const svcName = "manage-test-api-full-complete"
	sc := router.NewServiceContext(&fakeManageSvc{svcName})
	require.NotNil(t, sc)

	beginAPI := &manage.ClusterSwitchProvider{
		Action:         "begin",
		TargetProvider: "local",
	}
	beginReq := &mockRequest{serviceName: svcName}
	_, err := beginAPI.Do(beginReq)
	require.NoError(t, err, "API begin with local provider should succeed")

	completeAPI := &manage.ClusterSwitchProvider{Action: "complete"}
	completeReq := &mockRequest{serviceName: svcName}
	result, err := completeAPI.Do(completeReq)
	require.NoError(t, err)
	require.NotNil(t, result)
	status, ok := result.(*manage.ClusterSwitchProvider)
	require.True(t, ok)
	assert.Equal(t, "ok", status.Result)

	// After complete, ClusterProvider must equal the new Current().
	require.NotNil(t, sc.ClusterSwitcher)
	assert.Equal(t, sc.ClusterProvider, sc.ClusterSwitcher.Current(),
		"ClusterProvider should equal ClusterSwitcher.Current() after API begin→complete")
}

// TestClusterSwitchProvider_APIBeginThenRollback_Succeeds verifies the full
// begin→rollback flow when both actions go through the API.
func TestClusterSwitchProvider_APIBeginThenRollback_Succeeds(t *testing.T) {
	const svcName = "manage-test-api-full-rollback"
	sc := router.NewServiceContext(&fakeManageSvc{svcName})
	require.NotNil(t, sc)

	originalProvider := sc.ClusterProvider

	beginAPI := &manage.ClusterSwitchProvider{
		Action:         "begin",
		TargetProvider: "local",
	}
	_, err := beginAPI.Do(&mockRequest{serviceName: svcName})
	require.NoError(t, err, "API begin with local provider should succeed")

	rollbackAPI := &manage.ClusterSwitchProvider{Action: "rollback"}
	result, err := rollbackAPI.Do(&mockRequest{serviceName: svcName})
	require.NoError(t, err)
	require.NotNil(t, result)
	status, ok := result.(*manage.ClusterSwitchProvider)
	require.True(t, ok)
	assert.Equal(t, "ok", status.Result)

	// After rollback ClusterProvider should remain the original.
	assert.Same(t, originalProvider, sc.ClusterProvider,
		"ClusterProvider should not change after API begin→rollback")
}
