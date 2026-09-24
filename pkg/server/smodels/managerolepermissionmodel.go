// 本文件定义自定义 Manage 角色的结构化精确权限行和安全校验。
package smodels

import (
	"errors"
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

// ManageRolePermissionModel 保存自定义角色的一条精确 Manage 权限。
// 关系使用稳定业务键，不引用角色或菜单的数据库 ID。
type ManageRolePermissionModel struct {
	*entity.Model
	RoleCode string `json:"roleCode" gorm:"size:128;not null;index"`
	Service  string `json:"service" gorm:"size:128;not null;index"`
	Path     string `json:"path" gorm:"size:512;not null"`
	Command  string `json:"command" gorm:"size:128;not null"`
}

// NewManageRolePermissionModel 创建已初始化的精确权限模型。
func NewManageRolePermissionModel() *ManageRolePermissionModel {
	return &ManageRolePermissionModel{Model: entity.NewModel()}
}

// NewModel 补齐反射创建时的嵌入模型。
func (own *ManageRolePermissionModel) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}

// GetHash 使用四段结构化身份生成唯一哈希，避免超长数据库复合索引。
func (own *ManageRolePermissionModel) GetHash() string {
	roleCode := strings.ToLower(strings.TrimSpace(own.RoleCode))
	service := strings.ToLower(strings.TrimSpace(own.Service))
	path := strings.TrimSpace(own.Path)
	command := strings.ToLower(strings.TrimSpace(own.Command))
	if roleCode == "" || service == "" || path == "" || command == "" {
		if own.Model != nil {
			return own.Hashcode
		}
		return ""
	}
	return utils.HashCodes(strings.Join([]string{roleCode, service, path, command}, "\x00"))
}

// AddValid 校验新增精确权限。
func (own *ManageRolePermissionModel) AddValid() error {
	return own.validate()
}

// UpdateValid 禁止修改已保存权限的结构化身份。
func (own *ManageRolePermissionModel) UpdateValid(old interface{}) error {
	previous, ok := old.(*ManageRolePermissionModel)
	if !ok || previous == nil {
		return invalidManageRolePermission("previous permission is unavailable")
	}
	if own.permissionIdentity() != previous.permissionIdentity() {
		return invalidManageRolePermission("permission identity is immutable")
	}
	return own.validate()
}

// RemoveValid 允许删除自定义角色权限。
func (own *ManageRolePermissionModel) RemoveValid() error { return nil }

func (own *ManageRolePermissionModel) validate() error {
	rawCommand := strings.TrimSpace(own.Command)
	own.RoleCode = strings.ToLower(strings.TrimSpace(own.RoleCode))
	own.Service = strings.ToLower(strings.TrimSpace(own.Service))
	own.Path = strings.TrimSpace(own.Path)
	own.Command = strings.ToLower(rawCommand)

	if err := servertype.ValidateManageRoleCode(own.RoleCode); err != nil {
		return invalidManageRolePermission(err.Error())
	}
	if IsBuiltInManageRoleCode(own.RoleCode) {
		return invalidManageRolePermission("built-in roles do not store permission rows")
	}
	if !validManagePermissionName(own.Service) {
		return invalidManageRolePermission("service is invalid")
	}
	if !strings.HasPrefix(own.Path, "/api/manage/") {
		return invalidManageRolePermission("path is invalid")
	}
	if rawCommand != own.Command || !validManagePermissionName(own.Command) {
		return invalidManageRolePermission("command is invalid")
	}
	return nil
}

func (own *ManageRolePermissionModel) permissionIdentity() string {
	return strings.Join([]string{
		strings.ToLower(strings.TrimSpace(own.RoleCode)),
		strings.ToLower(strings.TrimSpace(own.Service)),
		strings.TrimSpace(own.Path),
		strings.ToLower(strings.TrimSpace(own.Command)),
	}, "\x00")
}

func validManagePermissionName(value string) bool {
	return value != "" && len(value) <= 128 && strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '.' && r != '_' && r != '-'
	}) == -1
}

func invalidManageRolePermission(detail string) error {
	return servertype.NewPublicError(
		servertype.ErrorKindValidation,
		servertype.PublicCodeValidation,
		"invalid manage permission",
		errors.New(detail),
	)
}
