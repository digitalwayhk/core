package adminrbac

import (
	"context"

	manageapi "github.com/digitalwayhk/core/examples/09-admin-manage-rbac/api/manage"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
)

const ServiceName = "adminrbac"

type AdminService struct {
	roleProvider *ManageRoleProvider
}

func NewAdminService(repository ...adminRepository) *AdminService {
	var selected adminRepository = newModelAdminRepository()
	if len(repository) > 0 && repository[0] != nil {
		selected = repository[0]
	}
	return &AdminService{roleProvider: NewManageRoleProvider(selected)}
}

func (*AdminService) ServiceName() string { return ServiceName }

func (*AdminService) GetLocaleTitle(locale string) string {
	if locale == "en-US" {
		return "Administration"
	}
	return "系统管理示例"
}

func (*AdminService) GetTitle() string { return "系统管理示例" }

func (*AdminService) Routers() []servertype.IRouter {
	routers := make([]servertype.IRouter, 0, 8)
	routers = append(routers, manageapi.NewAdminUserManage().Routers()...)
	routers = append(routers, manageapi.NewAdminUserRoleManage().Routers()...)
	return routers
}

func (own *AdminService) ResolveManagePrincipal(
	ctx context.Context,
	request servertype.ManagePrincipalRequest,
) (servertype.ManagePrincipal, error) {
	return own.roleProvider.ResolveManagePrincipal(ctx, request)
}
