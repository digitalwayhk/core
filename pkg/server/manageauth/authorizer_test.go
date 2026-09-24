// 本文件验证内置角色、自定义角色并集、动态权限读取和 fail-closed 授权策略。
package manageauth

import (
	"context"
	"errors"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/smodels"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

type authorizerStoreStub struct {
	roles       map[string]*smodels.ManageRoleModel
	permissions map[string]bool
	err         error
	calls       int
}

func (s *authorizerStoreStub) FindRole(context.Context, string) (*smodels.ManageRoleModel, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	for _, role := range s.roles {
		return role, nil
	}
	return nil, nil
}

func (s *authorizerStoreStub) HasPermission(
	_ context.Context,
	roleCode string,
	service string,
	path string,
	command string,
) (bool, error) {
	s.calls++
	if s.err != nil {
		return false, s.err
	}
	return s.permissions[permissionKey(roleCode, service, path, command)], nil
}

func TestAuthorizerBuiltInPoliciesAreDynamic(t *testing.T) {
	store := &authorizerStoreStub{err: errors.New("store must not be queried")}
	authorizer := NewAuthorizer(store)

	require.NoError(t, authorizer.Authorize(context.Background(), []servertype.ManageRoleRef{
		{Code: servertype.ManageRoleSystemAdmin},
	}, Request{Service: "orders", Path: "/api/manage/orders/ordermanage/remove", Command: "remove"}))
	require.NoError(t, authorizer.Authorize(context.Background(), []servertype.ManageRoleRef{
		{Code: servertype.ManageRoleViewer},
	}, Request{Service: "orders", Path: "/api/manage/orders/ordermanage/view", Command: "view"}))
	require.NoError(t, authorizer.Authorize(context.Background(), []servertype.ManageRoleRef{
		{Code: servertype.ManageRoleViewer},
	}, Request{Service: "orders", Path: "/api/manage/orders/ordermanage/search", Command: "search"}))
	require.Zero(t, store.calls)
}

func TestAuthorizerViewerRejectsWriteCommands(t *testing.T) {
	authorizer := NewAuthorizer(&authorizerStoreStub{})
	err := authorizer.Authorize(context.Background(), []servertype.ManageRoleRef{
		{Code: servertype.ManageRoleViewer},
	}, Request{Service: "orders", Path: "/api/manage/orders/ordermanage/add", Command: "add"})

	contract := servertype.ResolvePublicError(err)
	require.Equal(t, servertype.ErrorKindForbidden, contract.Kind)
	require.Equal(t, 403, contract.HTTPStatus)
	require.Equal(t, "permission denied", contract.Message)
}

func TestAuthorizerUsesExactCustomRolePermission(t *testing.T) {
	role := smodels.NewManageRoleModel()
	role.Code = "ops.approver"
	role.Enabled = true
	role.Policy = servertype.ManageRolePolicyExplicit
	store := &authorizerStoreStub{
		roles: map[string]*smodels.ManageRoleModel{role.Code: role},
		permissions: map[string]bool{
			permissionKey(role.Code, "orders", "/api/manage/orders/ordermanage/approve", "approve"): true,
		},
	}
	authorizer := NewAuthorizer(store)

	require.NoError(t, authorizer.Authorize(context.Background(), []servertype.ManageRoleRef{
		{Code: role.Code},
	}, Request{Service: "orders", Path: "/api/manage/orders/ordermanage/approve", Command: "approve"}))

	err := authorizer.Authorize(context.Background(), []servertype.ManageRoleRef{
		{Code: role.Code},
	}, Request{Service: "orders", Path: "/api/manage/orders/ordermanage/approve", Command: "edit"})
	require.Equal(t, servertype.ErrorKindForbidden, servertype.ResolvePublicError(err).Kind)
}

func TestAuthorizerUsesUnionOfRolesAndIgnoresDisabledRoles(t *testing.T) {
	disabled := smodels.NewManageRoleModel()
	disabled.Code = "ops.disabled"
	disabled.Policy = servertype.ManageRolePolicyExplicit
	allowed := smodels.NewManageRoleModel()
	allowed.Code = "ops.allowed"
	allowed.Enabled = true
	allowed.Policy = servertype.ManageRolePolicyExplicit
	store := &roleAwareStoreStub{
		roles: map[string]*smodels.ManageRoleModel{
			disabled.Code: disabled,
			allowed.Code:  allowed,
		},
		allowedRole: allowed.Code,
	}
	authorizer := NewAuthorizer(store)

	require.NoError(t, authorizer.Authorize(context.Background(), []servertype.ManageRoleRef{
		{Code: disabled.Code},
		{Code: allowed.Code},
	}, Request{Service: "orders", Path: "/api/manage/orders/ordermanage/edit", Command: "edit"}))
}

func TestAuthorizerFailsClosedOnStoreError(t *testing.T) {
	cause := errors.New("database unavailable at private-host")
	authorizer := NewAuthorizer(&authorizerStoreStub{err: cause})
	err := authorizer.Authorize(context.Background(), []servertype.ManageRoleRef{
		{Code: "ops.approver"},
	}, Request{Service: "orders", Path: "/api/manage/orders/ordermanage/approve", Command: "approve"})

	contract := servertype.ResolvePublicError(err)
	require.Equal(t, servertype.ErrorKindInternal, contract.Kind)
	require.Equal(t, 500, contract.HTTPStatus)
	require.Equal(t, "internal server error", contract.Message)
	require.ErrorIs(t, err, cause)
}

type roleAwareStoreStub struct {
	roles       map[string]*smodels.ManageRoleModel
	allowedRole string
}

func (s *roleAwareStoreStub) FindRole(_ context.Context, code string) (*smodels.ManageRoleModel, error) {
	return s.roles[code], nil
}

func (s *roleAwareStoreStub) HasPermission(_ context.Context, roleCode, _, _, _ string) (bool, error) {
	return roleCode == s.allowedRole, nil
}

func permissionKey(roleCode, service, path, command string) string {
	return roleCode + "\x00" + service + "\x00" + path + "\x00" + command
}
