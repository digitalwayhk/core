// 本文件组装 09 示例服务、管理员页面和 Manage 角色 Provider。
package adminrbac

import (
	"context"

	manageapi "github.com/digitalwayhk/core/examples/09-admin-manage-rbac/api/manage"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
)

// ServiceName 是 09 示例的稳定服务名。
const ServiceName = "adminrbac"

// AdminService 组合管理员页面并实现 IManageRoleProvider。
type AdminService struct {
	roleProvider *ManageRoleProvider
}

// NewAdminService 创建示例服务；测试可注入内存仓库。
func NewAdminService(repository ...adminRepository) *AdminService {
	var selected adminRepository = newModelAdminRepository()
	if len(repository) > 0 && repository[0] != nil {
		selected = repository[0]
	}
	return &AdminService{roleProvider: NewManageRoleProvider(selected)}
}

// ServiceName 返回路由和权限匹配使用的服务名。
func (*AdminService) ServiceName() string { return ServiceName }

// GetLocaleTitle 返回示例服务标题。
func (*AdminService) GetLocaleTitle(locale string) string {
	if locale == "en-US" {
		return "Administration"
	}
	return "系统管理示例"
}

// GetTitle 返回默认中文服务标题。
func (*AdminService) GetTitle() string { return "系统管理示例" }

// Routers 返回消费方管理员与角色关系页面。
func (*AdminService) Routers() []servertype.IRouter {
	routers := make([]servertype.IRouter, 0, 8)
	routers = append(routers, manageapi.NewAdminUserManage().Routers()...)
	routers = append(routers, manageapi.NewAdminUserRoleManage().Routers()...)
	return routers
}

// ResolveManagePrincipal 委托示例角色 Provider 解析可信管理员身份。
func (own *AdminService) ResolveManagePrincipal(
	ctx context.Context,
	request servertype.ManagePrincipalRequest,
) (servertype.ManagePrincipal, error) {
	return own.roleProvider.ResolveManagePrincipal(ctx, request)
}
