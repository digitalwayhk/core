package run

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/stretchr/testify/require"
)

type manageAuthTestRouter struct {
	info *types.RouterInfo
}

func (*manageAuthTestRouter) Parse(types.IRequest) error             { return nil }
func (*manageAuthTestRouter) Validation(types.IRequest) error        { return nil }
func (*manageAuthTestRouter) Do(types.IRequest) (interface{}, error) { return nil, nil }
func (r *manageAuthTestRouter) RouterInfo() *types.RouterInfo        { return r.info }

type manageAuthTestService struct {
	name  string
	route types.IRouter
}

func (s *manageAuthTestService) ServiceName() string { return s.name }
func (s *manageAuthTestService) Routers() []types.IRouter {
	if s.route == nil {
		return nil
	}
	return []types.IRouter{s.route}
}

func manageAuthTestContext(t *testing.T, name string) *router.ServiceContext {
	t.Helper()
	return manageAuthTestContextWithOptions(t, name, true, compatibleManageAuthConfig(name))
}

func plainServiceContext(t *testing.T, name string) *router.ServiceContext {
	t.Helper()
	return manageAuthTestContextWithOptions(t, name, false, compatibleManageAuthConfig(name))
}

func manageAuthTestContextWithOptions(t *testing.T, name string, withManage bool, cfg *config.ServerConfig) *router.ServiceContext {
	t.Helper()
	var route types.IRouter
	if withManage {
		path := "/api/manage/" + strings.ToLower(name) + "/manageauthtest"
		api := &manageAuthTestRouter{}
		info := &types.RouterInfo{
			ID:           utils.HashCode64(path),
			Path:         path,
			ServiceName:  name,
			PackPath:     "fixture/api/manage",
			Method:       http.MethodPost,
			PathType:     types.ManageType,
			InstanceName: "ManageAuthTest-" + name,
			StructName:   "manageAuthTestRouter",
		}
		api.info = info
		info.SetInstance(api)
		route = api
	}
	service := &manageAuthTestService{name: name, route: route}
	ctx := &router.ServiceContext{
		Config: cfg,
		Service: &types.Service{
			Name:     name,
			Routers:  service.Routers(),
			Instance: service,
		},
	}
	ctx.Router = router.NewServiceRouter(ctx, service)
	return ctx
}

func compatibleManageAuthConfig(name string) *config.ServerConfig {
	cfg := config.NewServiceDefaultConfig(name, 18080)
	cfg.ManageAuth.AccessSecret = "shared-manage-access-secret"
	cfg.ManageAuth.AccessExpire = 7200
	cfg.ManageAuth.RefreshSecret = "shared-manage-refresh-secret"
	cfg.ManageAuth.RefreshExpire = 2592000
	cfg.ManageAuth.CasDoor.Enable = false
	cfg.AuthRevocation.Mode = config.AuthRevocationModeLocal
	cfg.AuthRevocation.BadgerPath = filepath.Join("data", strings.ToLower(name), "auth-revocation")
	return cfg
}

func writeManageCasdoorYAML(t *testing.T, dir, filename, endpoint, clientID, clientSecret, cert, org, app, frontend string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o700))
	path := filepath.Join(dir, filename)
	content := "certificate: " + cert + "\nserver:\n" +
		"  endpoint: " + endpoint + "\n" +
		"  client_id: " + clientID + "\n" +
		"  client_secret: " + clientSecret + "\n" +
		"  organization: " + org + "\n" +
		"  application: " + app + "\n" +
		"  frontend_url: " + frontend + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func enableSharedCasdoor(t *testing.T, cfg *config.ServerConfig, yamlPath string) {
	t.Helper()
	cfg.ManageAuth.CasDoor.Enable = true
	cfg.ManageAuth.CasDoor.YamlFilePath = yamlPath
	cfg.ManageAuth.CasDoor.WebhookSecret = "shared-manage-webhook-secret"
	cfg.AuthRevocation.Mode = config.AuthRevocationModeShared
	cfg.AuthRevocation.Redis.Addr = "127.0.0.1:6379"
	cfg.AuthRevocation.Redis.Password = "shared-redis-password"
	cfg.AuthRevocation.Redis.Prefix = "core:authrevocation"
	cfg.AuthRevocation.BadgerPath = filepath.Join(t.TempDir(), cfg.Name, "auth-revocation")
}

func TestResolveManageAuthAuthority(t *testing.T) {
	t.Run("no manage services returns nil", func(t *testing.T) {
		contexts := []*router.ServiceContext{
			plainServiceContext(t, "alpha"),
			plainServiceContext(t, "beta"),
		}
		authority, err := resolveManageAuthAuthority(contexts, "")
		require.NoError(t, err)
		require.Nil(t, authority)
	})

	t.Run("single manage service auto selects", func(t *testing.T) {
		orders := manageAuthTestContext(t, "orders")
		plain := plainServiceContext(t, "metrics")
		authority, err := resolveManageAuthAuthority([]*router.ServiceContext{orders, plain}, "")
		require.NoError(t, err)
		require.NotNil(t, authority)
		require.Equal(t, "orders", authority.name)
		require.Same(t, orders, authority.context)
		require.Same(t, orders.Router, authority.router)
	})

	t.Run("multiple manage services require ManageAuthAuthorityService", func(t *testing.T) {
		contexts := []*router.ServiceContext{
			manageAuthTestContext(t, "orders"),
			manageAuthTestContext(t, "users"),
		}
		_, err := resolveManageAuthAuthority(contexts, "")
		require.Error(t, err)
		require.ErrorContains(t, err, "ManageAuthAuthorityService")
	})

	t.Run("configured service not found fails", func(t *testing.T) {
		contexts := []*router.ServiceContext{
			manageAuthTestContext(t, "orders"),
			manageAuthTestContext(t, "users"),
		}
		_, err := resolveManageAuthAuthority(contexts, "missing")
		require.Error(t, err)
		require.ErrorContains(t, err, "ManageAuthAuthorityService")
		require.ErrorContains(t, err, "missing")
	})

	t.Run("service name match is normalized", func(t *testing.T) {
		orders := manageAuthTestContext(t, "Orders")
		users := manageAuthTestContext(t, "Users")
		// Align secrets so selection can succeed after name match.
		users.Config.ManageAuth = orders.Config.ManageAuth
		authority, err := resolveManageAuthAuthority(
			[]*router.ServiceContext{orders, users},
			"  orders  ",
		)
		require.NoError(t, err)
		require.NotNil(t, authority)
		require.Equal(t, "Orders", authority.name)
	})

	t.Run("incompatible fields fail without leaking secrets", func(t *testing.T) {
		type fieldCase struct {
			name       string
			field      string
			secretLike string
			mutate     func(authority, other *router.ServiceContext)
		}
		cases := []fieldCase{
			{
				name:       "AccessSecret",
				field:      "ManageAuth.AccessSecret",
				secretLike: "orders-access-secret-leaked",
				mutate: func(authority, other *router.ServiceContext) {
					authority.Config.ManageAuth.AccessSecret = "orders-access-secret-leaked"
					other.Config.ManageAuth.AccessSecret = "users-access-secret-leaked"
				},
			},
			{
				name:  "AccessExpire",
				field: "ManageAuth.AccessExpire",
				mutate: func(authority, other *router.ServiceContext) {
					authority.Config.ManageAuth.AccessExpire = 7200
					other.Config.ManageAuth.AccessExpire = 3600
				},
			},
			{
				name:       "RefreshSecret",
				field:      "ManageAuth.RefreshSecret",
				secretLike: "orders-refresh-secret-leaked",
				mutate: func(authority, other *router.ServiceContext) {
					authority.Config.ManageAuth.RefreshSecret = "orders-refresh-secret-leaked"
					other.Config.ManageAuth.RefreshSecret = "users-refresh-secret-leaked"
				},
			},
			{
				name:  "RefreshExpire",
				field: "ManageAuth.RefreshExpire",
				mutate: func(authority, other *router.ServiceContext) {
					authority.Config.ManageAuth.RefreshExpire = 2592000
					other.Config.ManageAuth.RefreshExpire = 86400
				},
			},
			{
				name:  "CasDoor.Enable",
				field: "ManageAuth.CasDoor.Enable",
				mutate: func(authority, other *router.ServiceContext) {
					authority.Config.ManageAuth.CasDoor.Enable = false
					other.Config.ManageAuth.CasDoor.Enable = true
				},
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				orders := manageAuthTestContext(t, "orders-"+tc.name)
				users := manageAuthTestContext(t, "users-"+tc.name)
				users.Config.ManageAuth = orders.Config.ManageAuth
				tc.mutate(orders, users)

				_, err := resolveManageAuthAuthority(
					[]*router.ServiceContext{orders, users},
					"orders-"+tc.name,
				)
				require.Error(t, err)
				require.ErrorContains(t, err, tc.field)
				require.ErrorContains(t, err, "orders-"+tc.name)
				require.ErrorContains(t, err, "users-"+tc.name)
				if tc.secretLike != "" {
					require.NotContains(t, err.Error(), tc.secretLike)
					require.NotContains(t, err.Error(), "users-"+strings.TrimPrefix(tc.secretLike, "orders-"))
				}
			})
		}
	})

	t.Run("multi service casdoor requires shared redis contract", func(t *testing.T) {
		baseDir := t.TempDir()
		yamlA := writeManageCasdoorYAML(t, baseDir, "a.yaml",
			"http://127.0.0.1:18000", "client-id", "client-secret-value",
			"test-certificate", "org", "app", "http://localhost:3000")
		yamlB := writeManageCasdoorYAML(t, baseDir, "b.yaml",
			"http://127.0.0.1:18000", "client-id", "client-secret-value",
			"test-certificate", "org", "app", "http://localhost:3000")

		type redisCase struct {
			name   string
			field  string
			mutate func(other *router.ServiceContext)
			secret string
		}
		cases := []redisCase{
			{
				name:  "mode not shared",
				field: "AuthRevocation.Mode",
				mutate: func(other *router.ServiceContext) {
					other.Config.AuthRevocation.Mode = config.AuthRevocationModeLocal
				},
			},
			{
				name:  "redis addr",
				field: "AuthRevocation.Redis.Addr",
				mutate: func(other *router.ServiceContext) {
					other.Config.AuthRevocation.Redis.Addr = "127.0.0.1:6380"
				},
			},
			{
				name:   "redis password",
				field:  "AuthRevocation.Redis.Password",
				secret: "other-redis-password-leaked",
				mutate: func(other *router.ServiceContext) {
					other.Config.AuthRevocation.Redis.Password = "other-redis-password-leaked"
				},
			},
			{
				name:  "redis prefix",
				field: "AuthRevocation.Redis.Prefix",
				mutate: func(other *router.ServiceContext) {
					other.Config.AuthRevocation.Redis.Prefix = "other:prefix"
				},
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				ordersCfg := compatibleManageAuthConfig("orders-redis-" + tc.name)
				usersCfg := compatibleManageAuthConfig("users-redis-" + tc.name)
				enableSharedCasdoor(t, ordersCfg, yamlA)
				enableSharedCasdoor(t, usersCfg, yamlB)
				orders := manageAuthTestContextWithOptions(t, "orders-redis-"+tc.name, true, ordersCfg)
				users := manageAuthTestContextWithOptions(t, "users-redis-"+tc.name, true, usersCfg)
				tc.mutate(users)

				_, err := resolveManageAuthAuthority(
					[]*router.ServiceContext{orders, users},
					"orders-redis-"+tc.name,
				)
				require.Error(t, err)
				require.ErrorContains(t, err, tc.field)
				if tc.secret != "" {
					require.NotContains(t, err.Error(), tc.secret)
					require.NotContains(t, err.Error(), "shared-redis-password")
				}
			})
		}
	})

	t.Run("multi service casdoor loaded config must match", func(t *testing.T) {
		type casdoorCase struct {
			name   string
			field  string
			secret string
			writeB func(t *testing.T, dir string) string
			mutate func(authority, other *router.ServiceContext)
		}
		cases := []casdoorCase{
			{
				name:   "webhook secret",
				field:  "ManageAuth.CasDoor.WebhookSecret",
				secret: "other-webhook-secret-leaked",
				writeB: func(t *testing.T, dir string) string {
					return writeManageCasdoorYAML(t, dir, "b.yaml",
						"http://127.0.0.1:18000", "client-id", "client-secret-value",
						"test-certificate", "org", "app", "http://localhost:3000")
				},
				mutate: func(_, other *router.ServiceContext) {
					other.Config.ManageAuth.CasDoor.WebhookSecret = "other-webhook-secret-leaked"
				},
			},
			{
				name:  "endpoint",
				field: "ManageAuth.CasDoor.Endpoint",
				writeB: func(t *testing.T, dir string) string {
					return writeManageCasdoorYAML(t, dir, "b.yaml",
						"http://127.0.0.1:18001", "client-id", "client-secret-value",
						"test-certificate", "org", "app", "http://localhost:3000")
				},
			},
			{
				name:  "client id",
				field: "ManageAuth.CasDoor.ClientID",
				writeB: func(t *testing.T, dir string) string {
					return writeManageCasdoorYAML(t, dir, "b.yaml",
						"http://127.0.0.1:18000", "other-client-id", "client-secret-value",
						"test-certificate", "org", "app", "http://localhost:3000")
				},
			},
			{
				name:   "client secret",
				field:  "ManageAuth.CasDoor.ClientSecret",
				secret: "other-client-secret-leaked",
				writeB: func(t *testing.T, dir string) string {
					return writeManageCasdoorYAML(t, dir, "b.yaml",
						"http://127.0.0.1:18000", "client-id", "other-client-secret-leaked",
						"test-certificate", "org", "app", "http://localhost:3000")
				},
			},
			{
				name:   "certificate",
				field:  "ManageAuth.CasDoor.Certificate",
				secret: "other-certificate-leaked",
				writeB: func(t *testing.T, dir string) string {
					return writeManageCasdoorYAML(t, dir, "b.yaml",
						"http://127.0.0.1:18000", "client-id", "client-secret-value",
						"other-certificate-leaked", "org", "app", "http://localhost:3000")
				},
			},
			{
				name:  "organization",
				field: "ManageAuth.CasDoor.Organization",
				writeB: func(t *testing.T, dir string) string {
					return writeManageCasdoorYAML(t, dir, "b.yaml",
						"http://127.0.0.1:18000", "client-id", "client-secret-value",
						"test-certificate", "other-org", "app", "http://localhost:3000")
				},
			},
			{
				name:  "application",
				field: "ManageAuth.CasDoor.Application",
				writeB: func(t *testing.T, dir string) string {
					return writeManageCasdoorYAML(t, dir, "b.yaml",
						"http://127.0.0.1:18000", "client-id", "client-secret-value",
						"test-certificate", "org", "other-app", "http://localhost:3000")
				},
			},
			{
				name:  "frontend url",
				field: "ManageAuth.CasDoor.FrontendURL",
				writeB: func(t *testing.T, dir string) string {
					return writeManageCasdoorYAML(t, dir, "b.yaml",
						"http://127.0.0.1:18000", "client-id", "client-secret-value",
						"test-certificate", "org", "app", "http://localhost:4000")
				},
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				dir := t.TempDir()
				yamlA := writeManageCasdoorYAML(t, dir, "a.yaml",
					"http://127.0.0.1:18000", "client-id", "client-secret-value",
					"test-certificate", "org", "app", "http://localhost:3000")
				yamlB := tc.writeB(t, dir)
				ordersCfg := compatibleManageAuthConfig("orders-casdoor-" + tc.name)
				usersCfg := compatibleManageAuthConfig("users-casdoor-" + tc.name)
				enableSharedCasdoor(t, ordersCfg, yamlA)
				enableSharedCasdoor(t, usersCfg, yamlB)
				orders := manageAuthTestContextWithOptions(t, "orders-casdoor-"+tc.name, true, ordersCfg)
				users := manageAuthTestContextWithOptions(t, "users-casdoor-"+tc.name, true, usersCfg)
				if tc.mutate != nil {
					tc.mutate(orders, users)
				}

				_, err := resolveManageAuthAuthority(
					[]*router.ServiceContext{orders, users},
					"orders-casdoor-"+tc.name,
				)
				require.Error(t, err)
				require.ErrorContains(t, err, tc.field)
				if tc.secret != "" {
					require.NotContains(t, err.Error(), tc.secret)
				}
				require.NotContains(t, err.Error(), "client-secret-value")
				require.NotContains(t, err.Error(), "shared-manage-webhook-secret")
				require.NotContains(t, err.Error(), "test-certificate")
			})
		}
	})

	t.Run("yaml file path may differ when loaded content matches", func(t *testing.T) {
		dir := t.TempDir()
		yamlA := writeManageCasdoorYAML(t, dir, "orders.yaml",
			"http://127.0.0.1:18000", "client-id", "client-secret-value",
			"test-certificate", "org", "app", "http://localhost:3000")
		yamlB := writeManageCasdoorYAML(t, dir, "users.yaml",
			"http://127.0.0.1:18000", "client-id", "client-secret-value",
			"test-certificate", "org", "app", "http://localhost:3000")
		require.NotEqual(t, yamlA, yamlB)

		ordersCfg := compatibleManageAuthConfig("orders-yaml-path")
		usersCfg := compatibleManageAuthConfig("users-yaml-path")
		enableSharedCasdoor(t, ordersCfg, yamlA)
		enableSharedCasdoor(t, usersCfg, yamlB)
		orders := manageAuthTestContextWithOptions(t, "orders-yaml-path", true, ordersCfg)
		users := manageAuthTestContextWithOptions(t, "users-yaml-path", true, usersCfg)

		authority, err := resolveManageAuthAuthority(
			[]*router.ServiceContext{orders, users},
			"orders-yaml-path",
		)
		require.NoError(t, err)
		require.NotNil(t, authority)
		require.Equal(t, "orders-yaml-path", authority.name)
	})

	t.Run("authority revocation mode local fails closed", func(t *testing.T) {
		baseDir := t.TempDir()
		yamlA := writeManageCasdoorYAML(t, baseDir, "a.yaml",
			"http://127.0.0.1:18000", "client-id", "client-secret-value",
			"test-certificate", "org", "app", "http://localhost:3000")
		yamlB := writeManageCasdoorYAML(t, baseDir, "b.yaml",
			"http://127.0.0.1:18000", "client-id", "client-secret-value",
			"test-certificate", "org", "app", "http://localhost:3000")

		ordersCfg := compatibleManageAuthConfig("orders-auth-mode-local")
		usersCfg := compatibleManageAuthConfig("users-auth-mode-local")
		enableSharedCasdoor(t, ordersCfg, yamlA)
		enableSharedCasdoor(t, usersCfg, yamlB)
		// 权威服务自身不是 shared，非权威保持 shared。
		ordersCfg.AuthRevocation.Mode = config.AuthRevocationModeLocal
		ordersCfg.AuthRevocation.Redis.Password = "authority-redis-password-leaked"
		usersCfg.AuthRevocation.Redis.Password = "other-redis-password-leaked"

		orders := manageAuthTestContextWithOptions(t, "orders-auth-mode-local", true, ordersCfg)
		users := manageAuthTestContextWithOptions(t, "users-auth-mode-local", true, usersCfg)

		_, err := resolveManageAuthAuthority(
			[]*router.ServiceContext{orders, users},
			"orders-auth-mode-local",
		)
		require.Error(t, err)
		require.ErrorContains(t, err, "AuthRevocation.Mode")
		require.NotContains(t, err.Error(), "authority-redis-password-leaked")
		require.NotContains(t, err.Error(), "other-redis-password-leaked")
		require.NotContains(t, err.Error(), "shared-redis-password")
	})

	t.Run("casdoor config load failure fails closed without leaking secrets", func(t *testing.T) {
		baseDir := t.TempDir()
		validYAML := writeManageCasdoorYAML(t, baseDir, "valid.yaml",
			"http://127.0.0.1:18000", "client-id", "client-secret-value",
			"test-certificate", "org", "app", "http://localhost:3000")
		missingAuthorityYAML := filepath.Join(baseDir, "missing-authority-secret-path.yaml")
		missingOtherYAML := filepath.Join(baseDir, "missing-other-secret-path.yaml")

		t.Run("authority yaml unloadable", func(t *testing.T) {
			ordersCfg := compatibleManageAuthConfig("orders-casdoor-load-auth")
			usersCfg := compatibleManageAuthConfig("users-casdoor-load-auth")
			enableSharedCasdoor(t, ordersCfg, missingAuthorityYAML)
			enableSharedCasdoor(t, usersCfg, validYAML)
			ordersCfg.ManageAuth.CasDoor.WebhookSecret = "authority-webhook-secret-leaked"
			usersCfg.ManageAuth.CasDoor.WebhookSecret = "authority-webhook-secret-leaked"
			orders := manageAuthTestContextWithOptions(t, "orders-casdoor-load-auth", true, ordersCfg)
			users := manageAuthTestContextWithOptions(t, "users-casdoor-load-auth", true, usersCfg)

			_, err := resolveManageAuthAuthority(
				[]*router.ServiceContext{orders, users},
				"orders-casdoor-load-auth",
			)
			require.Error(t, err)
			require.ErrorContains(t, err, "orders-casdoor-load-auth")
			require.ErrorContains(t, err, "ManageAuth.CasDoor")
			assertManageAuthErrorDoesNotLeak(t, err.Error(), missingAuthorityYAML,
				"client-secret-value", "test-certificate", "authority-webhook-secret-leaked")
		})

		t.Run("other yaml unloadable", func(t *testing.T) {
			ordersCfg := compatibleManageAuthConfig("orders-casdoor-load-other")
			usersCfg := compatibleManageAuthConfig("users-casdoor-load-other")
			enableSharedCasdoor(t, ordersCfg, validYAML)
			enableSharedCasdoor(t, usersCfg, missingOtherYAML)
			ordersCfg.ManageAuth.CasDoor.WebhookSecret = "other-webhook-secret-leaked"
			usersCfg.ManageAuth.CasDoor.WebhookSecret = "other-webhook-secret-leaked"
			orders := manageAuthTestContextWithOptions(t, "orders-casdoor-load-other", true, ordersCfg)
			users := manageAuthTestContextWithOptions(t, "users-casdoor-load-other", true, usersCfg)

			_, err := resolveManageAuthAuthority(
				[]*router.ServiceContext{orders, users},
				"orders-casdoor-load-other",
			)
			require.Error(t, err)
			require.ErrorContains(t, err, "users-casdoor-load-other")
			require.ErrorContains(t, err, "ManageAuth.CasDoor")
			assertManageAuthErrorDoesNotLeak(t, err.Error(), missingOtherYAML,
				"client-secret-value", "test-certificate", "other-webhook-secret-leaked")
		})
	})
}

func assertManageAuthErrorDoesNotLeak(t *testing.T, message string, forbidden ...string) {
	t.Helper()
	for _, item := range forbidden {
		require.NotContains(t, message, item)
	}
	require.NotContains(t, strings.ToLower(message), "token")
	require.NotContains(t, message, "Bearer")
}

// TestWebServerInitializeRejectsInvalidManageAuthAuthority 证明 initializeServers
// 在 Config.Validate 之后执行 Manage Auth 门禁，失败时不发布 HTMLServer 或业务服务。
func TestWebServerInitializeRejectsInvalidManageAuthAuthority(t *testing.T) {
	orders := manageAuthInitContext(t, "init-orders")
	users := manageAuthInitContext(t, "init-users")
	// 对齐 ManageAuth 契约，确保失败点是缺少 ManageAuthAuthorityService，而非字段不兼容。
	users.Config.ManageAuth = orders.Config.ManageAuth

	server := bareWebServer()
	server.ViewPort = 18081
	server.saveConfig = func(*config.ServerConfig) error {
		t.Fatal("Manage Auth 门禁失败后不得保存配置")
		return nil
	}
	// 故意不设置 ManageAuthAuthorityService。

	constructed, err := server.initializeServers([]*router.ServiceContext{orders, users})
	require.Error(t, err)
	require.Nil(t, constructed)
	require.ErrorContains(t, err, "初始化开发视图管理认证失败")
	require.ErrorContains(t, err, "ManageAuthAuthorityService")
	require.Nil(t, server.htmlServerSnapshot(), "失败时不得发布 HTMLServer")
	require.Empty(t, orders.GetServers(), "失败时不得构造 HTTP/gRPC 服务")
	require.Empty(t, users.GetServers(), "失败时不得构造 HTTP/gRPC 服务")
	require.Nil(t, orders.Service.HttpServer)
	require.Nil(t, users.Service.HttpServer)
}

// TestWebServerInitializeSkipsManageAuthAuthorityWithoutView 证明 HTMLServer
// 被禁用时，正式 REST/gRPC 服务不依赖开发视图的 Manage Auth 权威选择。
func TestWebServerInitializeSkipsManageAuthAuthorityWithoutView(t *testing.T) {
	orders := manageAuthInitContext(t, "no-view-orders")
	users := manageAuthInitContext(t, "no-view-users")
	users.Config.ManageAuth = orders.Config.ManageAuth

	server := bareWebServer()
	server.ViewPort = 0
	authority, err := server.resolveViewManageAuthAuthority(
		[]*router.ServiceContext{orders, users},
	)
	require.NoError(t, err)
	require.Nil(t, authority)
}

func manageAuthInitContext(t *testing.T, name string) *router.ServiceContext {
	t.Helper()
	cfg := compatibleManageAuthConfig(name)
	cfg.Host = "127.0.0.1"
	cfg.DataCenterID = 1
	cfg.Port = 0
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "off"
	cfg.Transport.GRPC.Port = 0
	cfg.Transport.GRPC.Security = config.GRPCSecurityConfig{Mode: "insecure"}
	require.NoError(t, cfg.Validate())
	return manageAuthTestContextWithOptions(t, name, true, cfg)
}
