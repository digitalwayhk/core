package smodels

import "github.com/digitalwayhk/core/pkg/persistence/entity"

// UserPeermissionsModel 是早期按用户和菜单权限 ID 绑定的兼容模型。
//
// Deprecated: 新的 Manage 授权应由消费方保存用户与 RoleCode 的关系，并使用
// ManageRoleModel 与 ManageRolePermissionModel。该类型暂不删除，以保持源码兼容。
type UserPeermissionsModel struct {
	*entity.Model
	UserID             string `json:"userid"`             //用户ID
	PermissionsModelID uint   `json:"permissionsmodelid"` //权限ID
}

func NewUserPeermissionsModel() *UserPeermissionsModel {
	return &UserPeermissionsModel{
		Model: entity.NewModel(),
	}
}
func (own *UserPeermissionsModel) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}
