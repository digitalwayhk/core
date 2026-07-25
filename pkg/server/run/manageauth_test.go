package run

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/stretchr/testify/require"
)

type manageAuthorityTestRouter struct {
	info *types.RouterInfo
}

func (*manageAuthorityTestRouter) Parse(types.IRequest) error             { return nil }
func (*manageAuthorityTestRouter) Validation(types.IRequest) error        { return nil }
func (*manageAuthorityTestRouter) Do(types.IRequest) (interface{}, error) { return nil, nil }
func (r *manageAuthorityTestRouter) RouterInfo() *types.RouterInfo        { return r.info }

type manageAuthorityTestService struct {
	name  string
	route types.IRouter
}

func (s *manageAuthorityTestService) ServiceName() string { return s.name }
func (s *manageAuthorityTestService) Routers() []types.IRouter {
	if s.route == nil {
		return nil
	}
	return []types.IRouter{s.route}
}

func manageAuthorityContext(t *testing.T, name string, withManage bool) *router.ServiceContext {
	t.Helper()
	cfg := config.NewServiceDefaultConfig(name, 18080)
	cfg.ManageAuth.AccessSecret = "shared-manage-access"
	cfg.ManageAuth.AccessExpire = 7200
	cfg.ManageAuth.RefreshSecret = "shared-manage-refresh"
	cfg.ManageAuth.RefreshExpire = 2592000
	cfg.ManageAuth.CasDoor.Enable = false
	cfg.AuthRevocation.Mode = config.AuthRevocationModeLocal
	cfg.AuthRevocation.BadgerPath = filepath.Join(t.TempDir(), name, "auth-revocation")

	var route types.IRouter
	if withManage {
		path := "/api/manage/" + strings.ToLower(name) + "/authority-test"
		value := &manageAuthorityTestRouter{}
		info := &types.RouterInfo{
			ID:           utils.HashCode64(path),
			Path:         path,
			ServiceName:  name,
			Method:       http.MethodPost,
			PathType:     types.ManageType,
			InstanceName: "ManageAuthorityTest-" + name,
			StructName:   "manageAuthorityTestRouter",
		}
		value.info = info
		info.SetInstance(value)
		route = value
	}
	service := &manageAuthorityTestService{name: name, route: route}
	ctx := &router.ServiceContext{
		Config: cfg,
		Service: &types.Service{Name: name, Routers: service.Routers(), Instance: service},
	}
	ctx.Router = router.NewServiceRouter(ctx, service)
	return ctx
}

func TestResolveManageAuthAuthoritySelection(t *testing.T) {
	t.Run("no candidates", func(t *testing.T) {
		authority, err := resolveManageAuthAuthority(
			[]*router.ServiceContext{manageAuthorityContext(t, "plain", false)}, "",
		)
		require.NoError(t, err)
		require.Nil(t, authority)
	})

	t.Run("single candidate auto selected", func(t *testing.T) {
		orders := manageAuthorityContext(t, "orders", true)
		authority, err := resolveManageAuthAuthority([]*router.ServiceContext{orders}, "")
		require.NoError(t, err)
		require.Equal(t, "orders", authority.name)
		require.Same(t, orders, authority.context)
		require.Same(t, orders.Router, authority.router)
	})

	t.Run("multiple candidates require explicit selection", func(t *testing.T) {
		_, err := resolveManageAuthAuthority([]*router.ServiceContext{
			manageAuthorityContext(t, "orders", true),
			manageAuthorityContext(t, "users", true),
		}, "")
		require.ErrorContains(t, err, "多个 Manage 服务")
	})

	t.Run("configured plain service is rejected", func(t *testing.T) {
		_, err := resolveManageAuthAuthority([]*router.ServiceContext{
			manageAuthorityContext(t, "orders", true),
			manageAuthorityContext(t, "plain", false),
		}, "plain")
		require.ErrorContains(t, err, "不存在")
	})

	t.Run("selection is normalized", func(t *testing.T) {
		orders := manageAuthorityContext(t, "Orders", true)
		users := manageAuthorityContext(t, "Users", true)
		users.Config.ManageAuth = orders.Config.ManageAuth
		authority, err := resolveManageAuthAuthority(
			[]*router.ServiceContext{orders, users}, " orders ",
		)
		require.NoError(t, err)
		require.Equal(t, "Orders", authority.name)
	})
}

func TestManageAuthCompatibilityRejectsMismatchedFieldsWithoutSecrets(t *testing.T) {
	tests := []struct {
		name   string
		field  string
		mutate func(*router.ServiceContext)
	}{
		{"access secret", "ManageAuth.AccessSecret", func(ctx *router.ServiceContext) {
			ctx.Config.ManageAuth.AccessSecret = "do-not-leak-access"
		}},
		{"access expire", "ManageAuth.AccessExpire", func(ctx *router.ServiceContext) {
			ctx.Config.ManageAuth.AccessExpire++
		}},
		{"refresh secret", "ManageAuth.RefreshSecret", func(ctx *router.ServiceContext) {
			ctx.Config.ManageAuth.RefreshSecret = "do-not-leak-refresh"
		}},
		{"refresh expire", "ManageAuth.RefreshExpire", func(ctx *router.ServiceContext) {
			ctx.Config.ManageAuth.RefreshExpire++
		}},
		{"casdoor enabled", "ManageAuth.CasDoor.Enable", func(ctx *router.ServiceContext) {
			ctx.Config.ManageAuth.CasDoor.Enable = true
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			authority := manageAuthorityContext(t, "orders", true)
			other := manageAuthorityContext(t, "users", true)
			other.Config.ManageAuth = authority.Config.ManageAuth
			tc.mutate(other)

			_, err := resolveManageAuthAuthority(
				[]*router.ServiceContext{authority, other}, "orders",
			)
			require.ErrorContains(t, err, tc.field)
			require.NotContains(t, err.Error(), "do-not-leak")
			require.NotContains(t, err.Error(), "shared-manage-access")
			require.NotContains(t, err.Error(), "shared-manage-refresh")
		})
	}
}

func TestManageAuthCompatibilityRequiresSharedCasdoorContract(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "casdoor.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte(
		"certificate: certificate-secret\nserver:\n"+
			"  endpoint: http://127.0.0.1:18000\n"+
			"  client_id: client-id\n"+
			"  client_secret: client-secret\n"+
			"  organization: org\n"+
			"  application: app\n"+
			"  frontend_url: http://localhost:3000\n",
	), 0o600))

	authority := manageAuthorityContext(t, "orders", true)
	other := manageAuthorityContext(t, "users", true)
	for _, ctx := range []*router.ServiceContext{authority, other} {
		ctx.Config.ManageAuth.CasDoor.Enable = true
		ctx.Config.ManageAuth.CasDoor.YamlFilePath = yamlPath
		ctx.Config.ManageAuth.CasDoor.WebhookSecret = "webhook-secret"
		ctx.Config.AuthRevocation.Mode = config.AuthRevocationModeShared
		ctx.Config.AuthRevocation.Redis.Addr = "127.0.0.1:6379"
		ctx.Config.AuthRevocation.Redis.Password = "redis-secret"
		ctx.Config.AuthRevocation.Redis.Prefix = "core:auth"
	}
	other.Config.AuthRevocation.Mode = config.AuthRevocationModeLocal

	_, err := resolveManageAuthAuthority(
		[]*router.ServiceContext{authority, other}, "orders",
	)
	require.ErrorContains(t, err, "AuthRevocation.Mode")
	for _, secret := range []string{"certificate-secret", "client-secret", "webhook-secret", "redis-secret"} {
		require.NotContains(t, err.Error(), secret)
	}

	other.Config.AuthRevocation.Mode = config.AuthRevocationModeShared
	otherYAML := filepath.Join(dir, "other-casdoor.yaml")
	require.NoError(t, os.WriteFile(otherYAML, []byte(
		"certificate: certificate-secret\nserver:\n"+
			"  endpoint: http://127.0.0.1:18001\n"+
			"  client_id: client-id\n"+
			"  client_secret: client-secret\n"+
			"  organization: org\n"+
			"  application: app\n"+
			"  frontend_url: http://localhost:3000\n",
	), 0o600))
	other.Config.ManageAuth.CasDoor.YamlFilePath = otherYAML
	_, err = resolveManageAuthAuthority(
		[]*router.ServiceContext{authority, other}, "orders",
	)
	require.ErrorContains(t, err, "ManageAuth.CasDoor.Endpoint")
	for _, secret := range []string{"certificate-secret", "client-secret", "webhook-secret", "redis-secret"} {
		require.NotContains(t, err.Error(), secret)
	}
}

func TestSetManageAuthAuthorityIsStartupOnly(t *testing.T) {
	server := bareWebServer()
	server.beginInitialization()
	t.Cleanup(server.endInitialization)
	require.NoError(t, server.SetManageAuthAuthority(" Users "))
	require.Equal(t, "users", server.manageAuthAuthoritySnapshot())

	server.runStarted.Store(true)
	require.ErrorContains(t, server.SetManageAuthAuthority("orders"), "启动前")
	require.Equal(t, "users", server.manageAuthAuthoritySnapshot())
}

func TestSetManageAuthAuthoritySerializesWithStartup(t *testing.T) {
	server := bareWebServer()
	server.runMu.Lock()
	result := make(chan error, 1)
	go func() {
		result <- server.SetManageAuthAuthority("orders")
	}()
	select {
	case err := <-result:
		server.runMu.Unlock()
		t.Fatalf("setter 未与启动边界串行化，提前返回：%v", err)
	case <-time.After(20 * time.Millisecond):
	}
	server.runStarted.Store(true)
	server.runMu.Unlock()
	err := <-result

	require.ErrorContains(t, err, "启动前")
	require.Empty(t, server.manageAuthAuthoritySnapshot())
}

func TestManageAuthAuthorityInitializationFailsBeforePublishingServers(t *testing.T) {
	orders := manageAuthorityContext(t, "orders-init", true)
	users := manageAuthorityContext(t, "users-init", true)
	users.Config.ManageAuth = orders.Config.ManageAuth
	orders.Config.Port = 18080
	users.Config.Port = 18081
	orders.Config.Transport.GRPC.Port = 19080
	users.Config.Transport.GRPC.Port = 19081

	server := bareWebServer()
	server.Port = router.DEFAULTPORT
	server.saveConfig = func(*config.ServerConfig) error {
		t.Fatal("权威门禁失败后不得保存配置")
		return nil
	}

	constructed, err := server.initializeServers([]*router.ServiceContext{orders, users})
	require.ErrorContains(t, err, "初始化 Manage Auth 权威失败")
	require.Nil(t, constructed)
	require.Nil(t, server.htmlServerSnapshot())
	require.Empty(t, orders.GetServers())
	require.Empty(t, users.GetServers())
}
