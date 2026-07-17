package basedata

import (
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models/common"
	"github.com/digitalwayhk/core/pkg/utils"
)

type User struct {
	*common.BaseDataModel
	AuthUserID string `gorm:"not null;uniqueIndex" json:"-"`
	Name       string `gorm:"not null" json:"name"`
	Enabled    bool   `gorm:"not null" json:"enabled"`
}

func NewUser() *User { return &User{BaseDataModel: common.NewBaseDataModel()} }

func (u *User) NewModel() {
	if u.BaseDataModel == nil || u.UserServiceModel == nil || u.Model == nil {
		u.BaseDataModel = common.NewBaseDataModel()
	}
}

func (u *User) GetHash() string { return utils.HashCodes(strings.TrimSpace(u.AuthUserID)) }
