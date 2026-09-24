package models

import (
	"errors"
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

const BootstrapAdministratorSlot = "first-casdoor-manage-user"

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

func NewAdminUserModel() *AdminUserModel { return &AdminUserModel{Model: entity.NewModel()} }

func (own *AdminUserModel) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}

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

func (own *AdminUserModel) AddValid() error { return own.validate() }

func (own *AdminUserModel) UpdateValid(old interface{}) error {
	previous, ok := old.(*AdminUserModel)
	if !ok || previous == nil || own.Code != previous.Code ||
		own.Provider != previous.Provider || own.ProviderSubject != previous.ProviderSubject ||
		own.IsFirst != previous.IsFirst || bootstrapSlotValue(own.BootstrapSlot) != bootstrapSlotValue(previous.BootstrapSlot) {
		return invalidAdminModel("admin identity is immutable")
	}
	return own.validate()
}

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

func (*AdminUserModel) GetLocalDBName() string  { return "admin_rbac" }
func (*AdminUserModel) GetRemoteDBName() string { return "admin_rbac" }

func invalidAdminModel(detail string) error {
	return servertype.NewPublicError(
		servertype.ErrorKindValidation, 0, "invalid administrator", errors.New(detail),
	)
}
