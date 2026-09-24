// 本文件保留示例根包的模型兼容门面，具体模型实现位于 models 子包。
package adminrbac

import "github.com/digitalwayhk/core/examples/09-admin-manage-rbac/models"

// AdminUserModel 是 models.AdminUserModel 的示例根包兼容别名。
type AdminUserModel = models.AdminUserModel

// AdminUserRoleModel 是 models.AdminUserRoleModel 的示例根包兼容别名。
type AdminUserRoleModel = models.AdminUserRoleModel

// NewAdminUserModel 保留根包构造入口。
var NewAdminUserModel = models.NewAdminUserModel

// NewAdminUserRoleModel 保留根包构造入口。
var NewAdminUserRoleModel = models.NewAdminUserRoleModel
