package adminrbac

import (
	"testing"

	servertype "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

func TestAdminServiceImplementsRoleProviderAndManagementPages(t *testing.T) {
	service := NewAdminService(newMemoryAdminRepository())
	var _ servertype.IManageRoleProvider = service

	paths := make([]string, 0)
	for _, route := range service.Routers() {
		paths = append(paths, route.RouterInfo().GetPath())
	}
	require.Contains(t, paths, "/api/manage/09-admin-manage-rbac/adminusermanage/view")
	require.Contains(t, paths, "/api/manage/09-admin-manage-rbac/adminuserrolemanage/add")
}
