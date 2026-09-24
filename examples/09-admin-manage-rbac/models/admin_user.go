// 本文件定义消费方管理员身份模型及跨 authority 实例安全的首用户标记。
package models

import (
	"errors"
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

// BootstrapAdministratorSlot 是数据库中只允许出现一次的首管理员仲裁值。
const BootstrapAdministratorSlot = "first-casdoor-manage-user"

// AdminUserModel 保存消费方管理员身份，不保存权限明细。
type AdminUserModel struct {
	*entity.Model
	Code            string  `json:"code" gorm:"size:128;not null;uniqueIndex"`
	Username        string  `json:"username" gorm:"size:256"`
	Provider        string  `json:"provider" gorm:"size:64;not null"`
	ProviderSubject string  `json:"providerSubject" gorm:"size:256;not null;uniqueIndex"`
	Enabled         bool    `json:"enabled" gorm:"not null"`
	IsFirst         bool    `json:"isFirst" gorm:"not null"`
	BootstrapSlot   *string `json:"-" gorm:"size:64;uniqueIndex"`
}

// NewAdminUserModel 创建已初始化的管理员模型。
func NewAdminUserModel() *AdminUserModel { return &AdminUserModel{Model: entity.NewModel()} }

// NewModel 补齐反射创建时的嵌入模型。
func (own *AdminUserModel) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}

// GetHash 使用稳定用户 Code 生成唯一哈希。
func (own *AdminUserModel) GetHash() string {
	code := strings.TrimSpace(own.Code)
	if code == "" {
		if own.Model != nil {
			return own.Hashcode
		}
		return ""
	}
	return utils.HashCodes(code)
}

// AddValid 校验新增管理员身份与首用户标记。
func (own *AdminUserModel) AddValid() error { return own.validate() }

// UpdateValid 禁止修改管理员身份和首用户标记。
func (own *AdminUserModel) UpdateValid(old interface{}) error {
	previous, ok := old.(*AdminUserModel)
	if !ok || previous == nil || own.Code != previous.Code ||
		own.Provider != previous.Provider || own.ProviderSubject != previous.ProviderSubject ||
		own.IsFirst != previous.IsFirst || bootstrapSlotValue(own.BootstrapSlot) != bootstrapSlotValue(previous.BootstrapSlot) {
		return invalidAdminModel("admin identity is immutable")
	}
	return own.validate()
}

// RemoveValid 禁止删除 bootstrap 管理员。
func (own *AdminUserModel) RemoveValid() error {
	if own.IsFirst {
		return servertype.NewPublicError(
			servertype.ErrorKindForbidden, 0, "", errors.New("bootstrap administrator cannot be removed"),
		)
	}
	return nil
}

func (own *AdminUserModel) validate() error {
	own.Code = strings.TrimSpace(own.Code)
	own.Username = strings.TrimSpace(own.Username)
	own.Provider = strings.ToLower(strings.TrimSpace(own.Provider))
	own.ProviderSubject = strings.TrimSpace(own.ProviderSubject)
	if own.Code == "" || own.Provider != servertype.AuthProviderCasdoor || own.ProviderSubject == "" {
		return invalidAdminModel("admin identity is invalid")
	}
	if own.IsFirst != (bootstrapSlotValue(own.BootstrapSlot) == BootstrapAdministratorSlot) {
		return invalidAdminModel("bootstrap administrator marker is invalid")
	}
	return nil
}

func bootstrapSlotValue(slot *string) string {
	if slot == nil {
		return ""
	}
	return *slot
}

// GetLocalDBName 返回示例本地数据库名。
func (*AdminUserModel) GetLocalDBName() string { return "admin_rbac" }

// GetRemoteDBName 返回示例远端数据库名。
func (*AdminUserModel) GetRemoteDBName() string { return "admin_rbac" }

func invalidAdminModel(detail string) error {
	return servertype.NewPublicError(
		servertype.ErrorKindValidation, 0, "invalid administrator", errors.New(detail),
	)
}
