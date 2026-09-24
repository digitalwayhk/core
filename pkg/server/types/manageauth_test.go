package types

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type manageRoleProviderContractStub struct{}

func (manageRoleProviderContractStub) ResolveManagePrincipal(
	context.Context,
	ManagePrincipalRequest,
) (ManagePrincipal, error) {
	return ManagePrincipal{Roles: []ManageRoleRef{{Code: ManageRoleSystemAdmin}}}, nil
}

func TestManageRoleProviderContract(t *testing.T) {
	var provider IManageRoleProvider = manageRoleProviderContractStub{}
	principal, err := provider.ResolveManagePrincipal(context.Background(), ManagePrincipalRequest{
		Identity: AuthIdentity{UID: "admin-1", AuthType: AuthTypeManage},
		Source:   AuthSourceCallback,
		DefaultRoles: []ManageRoleRef{
			{Code: ManageRoleViewer},
		},
	})

	require.NoError(t, err)
	require.Equal(t, []ManageRoleRef{{Code: ManageRoleSystemAdmin}}, principal.Roles)
}

func TestNormalizeManageRoleRefs(t *testing.T) {
	roles, err := NormalizeManageRoleRefs([]ManageRoleRef{
		{Code: " ops.approver "},
		{Code: ManageRoleViewer},
		{Code: "ops.approver"},
	})

	require.NoError(t, err)
	require.Equal(t, []ManageRoleRef{
		{Code: ManageRoleViewer},
		{Code: "ops.approver"},
	}, roles)
}

func TestNormalizeManageRoleRefsRejectsInvalidCodes(t *testing.T) {
	tests := []struct {
		name string
		code string
	}{
		{name: "empty", code: ""},
		{name: "uppercase", code: "OPS.Admin"},
		{name: "separator", code: "ops/admin"},
		{name: "space", code: "ops admin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeManageRoleRefs([]ManageRoleRef{{Code: tt.code}})
			require.Error(t, err)
		})
	}
}

func TestNormalizeManageRoleRefsRejectsTooManyRoles(t *testing.T) {
	roles := make([]ManageRoleRef, MaxManageRoleCodes+1)
	for i := range roles {
		roles[i] = ManageRoleRef{Code: "role." + string(rune('a'+i))}
	}

	_, err := NormalizeManageRoleRefs(roles)
	require.Error(t, err)
}

func TestRouterInfoGetCommand(t *testing.T) {
	tests := []struct {
		name       string
		pathType   ApiType
		structName string
		want       string
	}{
		{name: "generic standard operation", pathType: ManageType, structName: "Add[example.com/app.Order]", want: "add"},
		{name: "pointer generic standard operation", pathType: ManageType, structName: "*manage.Search[example.com/app.Order]", want: "search"},
		{name: "custom command", pathType: ManageType, structName: "ApproveOrder", want: "approveorder"},
		{name: "non manage route", pathType: PrivateType, structName: "ApproveOrder", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &RouterInfo{PathType: tt.pathType, StructName: tt.structName}
			require.Equal(t, tt.want, info.GetCommand())
		})
	}
}
