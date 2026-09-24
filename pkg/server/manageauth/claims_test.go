package manageauth

import (
	"testing"

	servertype "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestManageRolesFromClaimsNormalizesRoleCodes(t *testing.T) {
	roles, err := ManageRolesFromClaims(map[string]interface{}{
		servertype.ManageRolesClaim: `["ops.approver","core.viewer","ops.approver"]`,
	})

	require.NoError(t, err)
	require.Equal(t, []servertype.ManageRoleRef{
		{Code: servertype.ManageRoleViewer},
		{Code: "ops.approver"},
	}, roles)
}

func TestManageRolesFromClaimsFailsClosedForMissingOrInvalidClaims(t *testing.T) {
	tests := []struct {
		name   string
		claims map[string]interface{}
	}{
		{name: "missing", claims: map[string]interface{}{}},
		{name: "wrong type", claims: map[string]interface{}{servertype.ManageRolesClaim: []interface{}{"core.viewer"}}},
		{name: "invalid json", claims: map[string]interface{}{servertype.ManageRolesClaim: `not-json`}},
		{name: "invalid code", claims: map[string]interface{}{servertype.ManageRolesClaim: `["OPS.Admin"]`}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ManageRolesFromClaims(tt.claims)
			require.Error(t, err)
		})
	}
}
