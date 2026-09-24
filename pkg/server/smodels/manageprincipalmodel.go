// 本文件定义 Core Manage 控制面的标准管理员身份模型。
package smodels

import (
	"errors"
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

// ManagePrincipalBootstrapSlot 是数据库中只允许出现一次的首管理员仲裁值。
const ManagePrincipalBootstrapSlot = "first-casdoor-manage-principal"

// ManagePrincipalModel 保存从可信 Manage Token 身份建立的管理员目录。
type ManagePrincipalModel struct {
	*entity.Model
	Code            string  `json:"code" gorm:"size:128;not null;uniqueIndex"`
	Username        string  `json:"username" gorm:"size:256"`
	Provider        string  `json:"provider" gorm:"size:64;not null"`
	ProviderSubject string  `json:"providerSubject" gorm:"size:256;not null;uniqueIndex"`
	Enabled         bool    `json:"enabled" gorm:"not null"`
	IsFirst         bool    `json:"isFirst" gorm:"not null"`
	BootstrapSlot   *string `json:"-" gorm:"size:64;uniqueIndex"`
}

// NewManagePrincipalModel 创建已初始化的管理员身份模型。
func NewManagePrincipalModel() *ManagePrincipalModel {
	return &ManagePrincipalModel{Model: entity.NewModel()}
}

// NewModel 补齐反射创建时的嵌入模型。
func (own *ManagePrincipalModel) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}

// GetHash 使用稳定管理员 Code 生成唯一哈希。
func (own *ManagePrincipalModel) GetHash() string {
	code := strings.TrimSpace(own.Code)
	if code == "" {
		if own.Model != nil {
			return own.Hashcode
		}
		return ""
	}
	return utils.HashCodes(code)
}

// AddValid 校验新增管理员身份。
func (own *ManagePrincipalModel) AddValid() error { return own.validate() }

// UpdateValid 禁止修改可信身份键和首管理员标记。
func (own *ManagePrincipalModel) UpdateValid(old interface{}) error {
	previous, ok := old.(*ManagePrincipalModel)
	if !ok || previous == nil || own.Code != previous.Code || own.Provider != previous.Provider ||
		own.ProviderSubject != previous.ProviderSubject || own.IsFirst != previous.IsFirst ||
		managePrincipalSlot(own.BootstrapSlot) != managePrincipalSlot(previous.BootstrapSlot) {
		return invalidManagePrincipal("manage principal identity is immutable")
	}
	if previous.IsFirst && !own.Enabled {
		return servertype.NewPublicError(
			servertype.ErrorKindForbidden,
			servertype.PublicCodeForbidden,
			"permission denied",
			errors.New("bootstrap manage principal cannot be disabled"),
		)
	}
	return own.validate()
}

// RemoveValid 禁止删除首个系统管理员。
func (own *ManagePrincipalModel) RemoveValid() error {
	if own.IsFirst {
		return servertype.NewPublicError(
			servertype.ErrorKindForbidden, 0, "", errors.New("bootstrap manage principal cannot be removed"),
		)
	}
	return nil
}

func (own *ManagePrincipalModel) validate() error {
	own.Code = strings.TrimSpace(own.Code)
	own.Username = strings.TrimSpace(own.Username)
	own.Provider = strings.ToLower(strings.TrimSpace(own.Provider))
	own.ProviderSubject = strings.TrimSpace(own.ProviderSubject)
	if own.Code == "" || len(own.Code) > 128 || own.Provider == "" || own.ProviderSubject == "" {
		return invalidManagePrincipal("manage principal identity is invalid")
	}
	if own.IsFirst != (managePrincipalSlot(own.BootstrapSlot) == ManagePrincipalBootstrapSlot) {
		return invalidManagePrincipal("bootstrap manage principal marker is invalid")
	}
	return nil
}

func managePrincipalSlot(slot *string) string {
	if slot == nil {
		return ""
	}
	return *slot
}

func invalidManagePrincipal(detail string) error {
	return servertype.NewPublicError(
		servertype.ErrorKindValidation, 0, "invalid manage principal", errors.New(detail),
	)
}
