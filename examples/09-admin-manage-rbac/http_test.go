// 本文件从 JWT 到 Router 响应验证 Manage RBAC 的真实 HTTP 授权闭环和失败边界。
package adminrbac

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	coremanageauth "github.com/digitalwayhk/core/pkg/server/manageauth"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/safe"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	resttransport "github.com/digitalwayhk/core/pkg/server/trans/rest"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestManageRBACOverHTTP(t *testing.T) {
	fixture := newRBACHTTPFixture(t, true)
	customRole := "ops.editor"
	fixture.store.roles[customRole] = explicitRole(customRole)
	fixture.store.permissions[fixture.permissionKey(customRole, "add")] = true

	tests := []struct {
		name       string
		roles      []servertype.ManageRoleRef
		command    string
		wantStatus int
		wantCalled int64
	}{
		{name: "viewer views", roles: roleRefs(servertype.ManageRoleViewer), command: "view", wantStatus: http.StatusOK, wantCalled: 1},
		{name: "viewer searches", roles: roleRefs(servertype.ManageRoleViewer), command: "search", wantStatus: http.StatusOK, wantCalled: 1},
		{name: "viewer cannot add", roles: roleRefs(servertype.ManageRoleViewer), command: "add", wantStatus: http.StatusForbidden},
		{name: "viewer cannot edit", roles: roleRefs(servertype.ManageRoleViewer), command: "edit", wantStatus: http.StatusForbidden},
		{name: "viewer cannot remove", roles: roleRefs(servertype.ManageRoleViewer), command: "remove", wantStatus: http.StatusForbidden},
		{name: "custom role exact permission", roles: roleRefs(customRole), command: "add", wantStatus: http.StatusOK, wantCalled: 1},
		{name: "custom role other command denied", roles: roleRefs(customRole), command: "edit", wantStatus: http.StatusForbidden},
		{name: "system administrator writes", roles: roleRefs(servertype.ManageRoleSystemAdmin), command: "remove", wantStatus: http.StatusOK, wantCalled: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			before := fixture.calls[test.command].Load()
			response := fixture.request(t, test.command, test.roles, true)

			require.Equal(t, test.wantStatus, response.Code, response.Body.String())
			require.Equal(t, before+test.wantCalled, fixture.calls[test.command].Load())
			if test.wantStatus == http.StatusForbidden {
				require.JSONEq(t, `{"success":false,"code":40300,"message":"permission denied"}`, response.Body.String())
				require.NotContains(t, response.Body.String(), "manage command executed")
			} else {
				require.Contains(t, response.Body.String(), `"success":true`)
				require.Contains(t, response.Body.String(), `"command":"`+test.command+`"`)
			}
		})
	}
}

func TestManageRBACPermissionChangeAffectsNextHTTPRequest(t *testing.T) {
	fixture := newRBACHTTPFixture(t, true)
	roleCode := "ops.editor"
	fixture.store.roles[roleCode] = explicitRole(roleCode)

	denied := fixture.request(t, "edit", roleRefs(roleCode), true)
	require.Equal(t, http.StatusForbidden, denied.Code)
	require.Zero(t, fixture.calls["edit"].Load())

	fixture.store.permissions[fixture.permissionKey(roleCode, "edit")] = true
	allowed := fixture.request(t, "edit", roleRefs(roleCode), true)
	require.Equal(t, http.StatusOK, allowed.Code, allowed.Body.String())
	require.Equal(t, int64(1), fixture.calls["edit"].Load())
}

func TestManageRBACStoreFailureFailsClosedOverHTTP(t *testing.T) {
	fixture := newRBACHTTPFixture(t, true)
	fixture.store.err = errors.New("database unavailable at private-host")

	response := fixture.request(t, "edit", roleRefs("ops.editor"), true)

	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.JSONEq(t, `{"success":false,"code":50000,"message":"internal server error"}`, response.Body.String())
	require.NotContains(t, response.Body.String(), "private-host")
	require.Zero(t, fixture.calls["edit"].Load())
}

func TestManageRBACProviderAbsentKeepsLegacyHTTPCompatibility(t *testing.T) {
	fixture := newRBACHTTPFixture(t, false)

	response := fixture.request(t, "add", nil, false)

	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, int64(1), fixture.calls["add"].Load())
}

type rbacHTTPFixture struct {
	context *router.ServiceContext
	routes  map[string]*servertype.RouterInfo
	calls   map[string]*atomic.Int64
	store   *rbacHTTPStore
	secret  string
}

func newRBACHTTPFixture(t *testing.T, enabled bool) *rbacHTTPFixture {
	t.Helper()
	const serviceName = "adminrbac"
	fixture := &rbacHTTPFixture{
		routes: make(map[string]*servertype.RouterInfo),
		calls:  make(map[string]*atomic.Int64),
		store: &rbacHTTPStore{
			roles:       make(map[string]*smodels.ManageRoleModel),
			permissions: make(map[string]bool),
		},
		secret: "manage-rbac-http-access-secret",
	}
	routes := make([]servertype.IRouter, 0, 5)
	for _, command := range []string{"view", "search", "add", "edit", "remove"} {
		counter := &atomic.Int64{}
		path := "/api/manage/" + serviceName + "/rbacprobemanage/" + command
		probe := &rbacHTTPProbe{command: command, calls: counter}
		info := &servertype.RouterInfo{
			Path: path, Method: http.MethodPost, Auth: true,
			ServiceName: serviceName, PathType: servertype.ManageType,
			PackPath:     "examples/09-admin-manage-rbac/api/manage",
			StructName:   strings.ToUpper(command[:1]) + command[1:],
			InstanceName: "RBACProbeManage",
		}
		probe.info = info
		info.SetInstance(probe)
		fixture.routes[command] = info
		fixture.calls[command] = counter
		routes = append(routes, probe)
	}
	service := &rbacHTTPService{name: serviceName, routes: routes}
	cfg := config.NewServiceDefaultConfig(serviceName, 18991)
	cfg.ManageAuth.AccessSecret = fixture.secret
	cfg.ManageAuth.AccessExpire = 3600
	fixture.context = &router.ServiceContext{
		Config: cfg,
		Service: &servertype.Service{
			Name: serviceName, Instance: service, Routers: routes,
		},
	}
	fixture.context.Router = router.NewServiceRouter(fixture.context, service)
	if enabled {
		fixture.context.ManageRoleProvider = NewManageRoleProvider(newMemoryAdminRepository())
		fixture.context.ManageAuthorizer = coremanageauth.NewAuthorizer(fixture.store)
	}
	for command, info := range fixture.routes {
		fixture.routes[command] = fixture.context.Router.GetRouter(info.Path)
		require.NotNil(t, fixture.routes[command])
	}
	return fixture
}

func (own *rbacHTTPFixture) request(
	t *testing.T,
	command string,
	roles []servertype.ManageRoleRef,
	includeRoleClaim bool,
) *httptest.ResponseRecorder {
	t.Helper()
	info := own.routes[command]
	identity := servertype.AuthIdentity{UID: "manager-1", AuthType: servertype.AuthTypeManage}
	claims := safe.NewClaims(identity.UID, "manager")
	if includeRoleClaim {
		require.NoError(t, claims.SetManageRoles(roles))
	}
	pair, err := safe.IssueTokenPair(safe.TokenIssueRequest{
		Claims: claims, Identity: identity, AuthType: identity.AuthType,
		IssuedAt:     time.Now().UTC().Add(-time.Second),
		AccessSecret: own.secret, AccessExpireSeconds: 3600,
	})
	require.NoError(t, err)
	request := httptest.NewRequest(info.GetMethod(), info.GetPath(), nil)
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	response := httptest.NewRecorder()
	resttransport.NewExternalRouterHandler(own.context, info).ServeHTTP(response, request)
	return response
}

func (own *rbacHTTPFixture) permissionKey(roleCode, command string) string {
	info := own.routes[command]
	return strings.Join([]string{roleCode, "adminrbac", info.GetPath(), command}, "\x00")
}

type rbacHTTPService struct {
	name   string
	routes []servertype.IRouter
}

func (own *rbacHTTPService) ServiceName() string { return own.name }
func (own *rbacHTTPService) Routers() []servertype.IRouter {
	return own.routes
}

type rbacHTTPProbe struct {
	info    *servertype.RouterInfo
	command string
	calls   *atomic.Int64
}

func (own *rbacHTTPProbe) New(interface{}) servertype.IRouter {
	return &rbacHTTPProbe{info: own.info, command: own.command, calls: own.calls}
}
func (*rbacHTTPProbe) Parse(servertype.IRequest) error      { return nil }
func (*rbacHTTPProbe) Validation(servertype.IRequest) error { return nil }
func (own *rbacHTTPProbe) Do(servertype.IRequest) (interface{}, error) {
	own.calls.Add(1)
	return map[string]string{"command": own.command, "result": "manage command executed"}, nil
}
func (own *rbacHTTPProbe) RouterInfo() *servertype.RouterInfo { return own.info }

type rbacHTTPStore struct {
	roles       map[string]*smodels.ManageRoleModel
	permissions map[string]bool
	err         error
}

func (own *rbacHTTPStore) FindRole(_ context.Context, code string) (*smodels.ManageRoleModel, error) {
	if own.err != nil {
		return nil, own.err
	}
	return own.roles[code], nil
}

func (own *rbacHTTPStore) HasPermission(_ context.Context, roleCode, service, path, command string) (bool, error) {
	if own.err != nil {
		return false, own.err
	}
	return own.permissions[strings.Join([]string{roleCode, service, path, command}, "\x00")], nil
}

func explicitRole(code string) *smodels.ManageRoleModel {
	role := smodels.NewManageRoleModel()
	role.Code = code
	role.Enabled = true
	role.Policy = servertype.ManageRolePolicyExplicit
	return role
}

func roleRefs(codes ...string) []servertype.ManageRoleRef {
	roles := make([]servertype.ManageRoleRef, 0, len(codes))
	for _, code := range codes {
		roles = append(roles, servertype.ManageRoleRef{Code: code})
	}
	return roles
}
