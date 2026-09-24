// 本文件验证 WebServer 在监听前初始化唯一控制面存储并向业务服务绑定同一授权器。
package run

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/internal/managestore"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

type controlPlaneRoleProvider struct{}

func (controlPlaneRoleProvider) ResolveManagePrincipal(
	context.Context,
	types.ManagePrincipalRequest,
) (types.ManagePrincipal, error) {
	return types.ManagePrincipal{}, nil
}

// TestWebServerInitializesManageControlPlaneFromServerConfig 验证 server.json 是控制面存储的唯一配置源。
func TestWebServerInitializesManageControlPlaneFromServerConfig(t *testing.T) {
	resetControlPlaneStoreForTest(t)
	cfg := config.NewServiceDefaultConfig("server", 18080)
	cfg.ManageStore.Database = filepath.Join(t.TempDir(), "web_control_plane")

	web := &WebServer{}
	require.NoError(t, web.initializeManageControlPlane(cfg))
	require.NotNil(t, web.manageControlPlane)
	require.NotNil(t, web.manageControlPlane.Authorizer())
}

// TestWebServerBindsDefaultManageAuthorization 验证未自定义 Provider 的服务使用 Core 默认主体目录。
func TestWebServerBindsDefaultManageAuthorization(t *testing.T) {
	resetControlPlaneStoreForTest(t)
	cfg := config.NewServiceDefaultConfig("server", 18080)
	cfg.ManageStore.Database = filepath.Join(t.TempDir(), "shared_authorizer")
	web := &WebServer{}
	require.NoError(t, web.initializeManageControlPlane(cfg))

	first := &router.ServiceContext{}
	second := &router.ServiceContext{}
	web.bindManageAuthorization(first)
	web.bindManageAuthorization(second)

	require.Same(t, web.manageControlPlane.PrincipalProvider(), first.ManageRoleProvider)
	require.Same(t, first.ManageRoleProvider, second.ManageRoleProvider)
	require.Same(t, web.manageControlPlane.Authorizer(), first.ManageAuthorizer)
	require.Same(t, first.ManageAuthorizer, second.ManageAuthorizer)
}

// TestWebServerPreservesCustomManageRoleProvider 验证外部 IAM 可以显式覆盖 Core 默认主体解析。
func TestWebServerPreservesCustomManageRoleProvider(t *testing.T) {
	resetControlPlaneStoreForTest(t)
	cfg := config.NewServiceDefaultConfig("server", 18080)
	cfg.ManageStore.Database = filepath.Join(t.TempDir(), "custom_provider")
	web := &WebServer{}
	require.NoError(t, web.initializeManageControlPlane(cfg))
	custom := controlPlaneRoleProvider{}
	service := &router.ServiceContext{ManageRoleProvider: custom}

	web.bindManageAuthorization(service)

	require.Equal(t, custom, service.ManageRoleProvider)
	require.Same(t, web.manageControlPlane.Authorizer(), service.ManageAuthorizer)
}

func resetControlPlaneStoreForTest(t *testing.T) {
	t.Helper()
	managestore.ResetForTesting()
	t.Cleanup(managestore.ResetForTesting)
}
