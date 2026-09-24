package smodels

import (
	"errors"
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

// ManageRoleModel 是 Core Manage 域的角色目录。
// Code 是 Token、消费方用户关系及权限明细共同使用的公开稳定键。
type ManageRoleModel struct {
	*entity.Model
	Code        string `json:"code" gorm:"size:128;not null;uniqueIndex"`
	Name        string `json:"name" gorm:"size:256;not null"`
	Description string `json:"description" gorm:"size:1024"`
	Enabled     bool   `json:"enabled" gorm:"not null"`
	IsSystem    bool   `json:"isSystem" gorm:"not null"`
	IsDefault   bool   `json:"isDefault" gorm:"not null"`
	Policy      string `json:"policy" gorm:"size:32;not null"`
}

func NewManageRoleModel() *ManageRoleModel {
	return &ManageRoleModel{Model: entity.NewModel()}
}

func (own *ManageRoleModel) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}

func (own *ManageRoleModel) GetHash() string {
	code := strings.ToLower(strings.TrimSpace(own.Code))
	if code == "" {
		if own.Model != nil {
			return own.Hashcode
		}
		return ""
	}
	return utils.HashCodes(code)
}

func (own *ManageRoleModel) AddValid() error {
	return own.validate()
}

func (own *ManageRoleModel) UpdateValid(old interface{}) error {
	previous, ok := old.(*ManageRoleModel)
	if !ok || previous == nil {
		return invalidManageRole("previous role is unavailable")
	}
	if strings.TrimSpace(own.Code) != strings.TrimSpace(previous.Code) {
		return invalidManageRole("role code is immutable")
	}
	if IsBuiltInManageRoleCode(previous.Code) && (own.Policy != previous.Policy ||
		own.Enabled != previous.Enabled ||
		own.IsSystem != previous.IsSystem ||
		own.IsDefault != previous.IsDefault) {
		return protectedManageRole("built-in role policy is immutable")
	}
	return own.validate()
}

func (own *ManageRoleModel) RemoveValid() error {
	if IsBuiltInManageRoleCode(own.Code) || own.IsSystem {
		return protectedManageRole("built-in role cannot be removed")
	}
	return nil
}

func (own *ManageRoleModel) validate() error {
	own.Code = strings.ToLower(strings.TrimSpace(own.Code))
	own.Name = strings.TrimSpace(own.Name)
	own.Description = strings.TrimSpace(own.Description)
	if err := servertype.ValidateManageRoleCode(own.Code); err != nil {
		return invalidManageRole(err.Error())
	}
	if own.Name == "" {
		return invalidManageRole("role name is required")
	}

	switch own.Code {
	case servertype.ManageRoleSystemAdmin:
		if own.Policy != servertype.ManageRolePolicyGrantAll || !own.Enabled || !own.IsSystem || own.IsDefault {
			return protectedManageRole("system administrator role definition is immutable")
		}
	case servertype.ManageRoleViewer:
		if own.Policy != servertype.ManageRolePolicyReadOnly || !own.Enabled || !own.IsSystem || !own.IsDefault {
			return protectedManageRole("viewer role definition is immutable")
		}
	default:
		if own.IsSystem || own.Policy != servertype.ManageRolePolicyExplicit {
			return invalidManageRole("custom roles must use explicit policy")
		}
	}
	return nil
}

func IsBuiltInManageRoleCode(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case servertype.ManageRoleSystemAdmin, servertype.ManageRoleViewer:
		return true
	default:
		return false
	}
}

func NewBuiltInManageRoles() []*ManageRoleModel {
	admin := NewManageRoleModel()
	admin.Code = servertype.ManageRoleSystemAdmin
	admin.Name = "System administrator"
	admin.Description = "Built-in role with access to every Manage command"
	admin.Enabled = true
	admin.IsSystem = true
	admin.Policy = servertype.ManageRolePolicyGrantAll

	viewer := NewManageRoleModel()
	viewer.Code = servertype.ManageRoleViewer
	viewer.Name = "Viewer"
	viewer.Description = "Built-in role with view and search access"
	viewer.Enabled = true
	viewer.IsSystem = true
	viewer.IsDefault = true
	viewer.Policy = servertype.ManageRolePolicyReadOnly
	return []*ManageRoleModel{admin, viewer}
}

func invalidManageRole(detail string) error {
	return servertype.NewPublicError(
		servertype.ErrorKindValidation,
		servertype.PublicCodeValidation,
		"invalid manage role",
		errors.New(detail),
	)
}

func protectedManageRole(detail string) error {
	return servertype.NewPublicError(
		servertype.ErrorKindForbidden,
		servertype.PublicCodeForbidden,
		"permission denied",
		errors.New(detail),
	)
}
