package models

import (
	"errors"
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

type AdminUserRoleModel struct {
	*entity.Model
	UserCode string                   `json:"userCode" gorm:"size:128;not null;uniqueIndex:idx_admin_user_role"`
	RoleCode string                   `json:"roleCode" gorm:"size:128;not null;uniqueIndex:idx_admin_user_role"`
	User     *AdminUserModel          `json:"user,omitempty" gorm:"-"`
	Role     *smodels.ManageRoleModel `json:"role,omitempty" gorm:"-"`
}

func NewAdminUserRoleModel() *AdminUserRoleModel {
	return &AdminUserRoleModel{Model: entity.NewModel()}
}

func (own *AdminUserRoleModel) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}

func (own *AdminUserRoleModel) GetHash() string {
	userCode := strings.TrimSpace(own.UserCode)
	roleCode := strings.ToLower(strings.TrimSpace(own.RoleCode))
	if userCode == "" || roleCode == "" {
		if own.Model != nil {
			return own.Hashcode
		}
		return ""
	}
	return utils.HashCodes(userCode + "\x00" + roleCode)
}

func (own *AdminUserRoleModel) AddValid() error { return own.validate() }

func (own *AdminUserRoleModel) UpdateValid(old interface{}) error {
	previous, ok := old.(*AdminUserRoleModel)
	if !ok || previous == nil || own.UserCode != previous.UserCode || own.RoleCode != previous.RoleCode {
		return invalidAdminRole("administrator role identity is immutable")
	}
	return own.validate()
}

func (own *AdminUserRoleModel) RemoveValid() error { return nil }

func (own *AdminUserRoleModel) validate() error {
	own.UserCode = strings.TrimSpace(own.UserCode)
	own.RoleCode = strings.ToLower(strings.TrimSpace(own.RoleCode))
	if own.UserCode == "" {
		return invalidAdminRole("administrator code is required")
	}
	if err := servertype.ValidateManageRoleCode(own.RoleCode); err != nil {
		return invalidAdminRole(err.Error())
	}
	return nil
}

func (*AdminUserRoleModel) GetLocalDBName() string  { return "admin_rbac" }
func (*AdminUserRoleModel) GetRemoteDBName() string { return "admin_rbac" }

func invalidAdminRole(detail string) error {
	return servertype.NewPublicError(
		servertype.ErrorKindValidation, 0, "invalid administrator role", errors.New(detail),
	)
}
