// 本文件定义 Core 管理员与稳定 RoleCode 的多对多关系。
package smodels

import (
	"errors"
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

// ManagePrincipalRoleModel 仅保存管理员 Code 与角色 Code，不保存数据库 ID。
type ManagePrincipalRoleModel struct {
	*entity.Model
	PrincipalCode string                `json:"principalCode" gorm:"size:128;not null;uniqueIndex:idx_manage_principal_role"`
	RoleCode      string                `json:"roleCode" gorm:"size:128;not null;uniqueIndex:idx_manage_principal_role"`
	IsBootstrap   bool                  `json:"-" gorm:"not null"`
	Principal     *ManagePrincipalModel `json:"principal,omitempty" gorm:"-"`
	Role          *ManageRoleModel      `json:"role,omitempty" gorm:"-"`
}

// NewManagePrincipalRoleModel 创建已初始化的管理员角色关系。
func NewManagePrincipalRoleModel() *ManagePrincipalRoleModel {
	return &ManagePrincipalRoleModel{Model: entity.NewModel()}
}

// NewModel 补齐反射创建时的嵌入模型。
func (own *ManagePrincipalRoleModel) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}

// GetHash 使用 PrincipalCode 与 RoleCode 生成关系唯一哈希。
func (own *ManagePrincipalRoleModel) GetHash() string {
	principalCode := strings.TrimSpace(own.PrincipalCode)
	roleCode := strings.ToLower(strings.TrimSpace(own.RoleCode))
	if principalCode == "" || roleCode == "" {
		if own.Model != nil {
			return own.Hashcode
		}
		return ""
	}
	return utils.HashCodes(principalCode + "\x00" + roleCode)
}

// AddValid 校验新增管理员角色关系。
func (own *ManagePrincipalRoleModel) AddValid() error { return own.validate() }

// UpdateValid 禁止修改关系稳定键。
func (own *ManagePrincipalRoleModel) UpdateValid(old interface{}) error {
	previous, ok := old.(*ManagePrincipalRoleModel)
	if !ok || previous == nil || own.PrincipalCode != previous.PrincipalCode || own.RoleCode != previous.RoleCode ||
		own.IsBootstrap != previous.IsBootstrap {
		return invalidManagePrincipalRole("manage principal role identity is immutable")
	}
	return own.validate()
}

// RemoveValid 禁止解除首管理员的引导系统管理员关系。
func (own *ManagePrincipalRoleModel) RemoveValid() error {
	if own.IsBootstrap {
		return servertype.NewPublicError(
			servertype.ErrorKindForbidden,
			servertype.PublicCodeForbidden,
			"permission denied",
			errors.New("bootstrap manage principal role cannot be removed"),
		)
	}
	return nil
}

func (own *ManagePrincipalRoleModel) validate() error {
	own.PrincipalCode = strings.TrimSpace(own.PrincipalCode)
	own.RoleCode = strings.ToLower(strings.TrimSpace(own.RoleCode))
	if own.PrincipalCode == "" || len(own.PrincipalCode) > 128 {
		return invalidManagePrincipalRole("manage principal code is invalid")
	}
	if err := servertype.ValidateManageRoleCode(own.RoleCode); err != nil {
		return invalidManagePrincipalRole(err.Error())
	}
	if own.IsBootstrap && own.RoleCode != servertype.ManageRoleSystemAdmin {
		return invalidManagePrincipalRole("bootstrap relation must use the system administrator role")
	}
	return nil
}

func invalidManagePrincipalRole(detail string) error {
	return servertype.NewPublicError(
		servertype.ErrorKindValidation, 0, "invalid manage principal role", errors.New(detail),
	)
}
