// 本文件定义当前服务基础资料模型及其持久化能力。
package basedata

import (
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models/common"
	"github.com/digitalwayhk/core/pkg/utils"
)

// User 定义本文件能力使用的核心结构。
type User struct {
	*common.BaseDataModel
	AuthUserID string `gorm:"not null;uniqueIndex" json:"-"`
	Name       string `gorm:"not null" json:"name"`
	Enabled    bool   `gorm:"not null" json:"enabled"`
}

// NewUser 执行本文件能力对应的业务操作。
func NewUser() *User { return &User{BaseDataModel: common.NewBaseDataModel()} }

// NewModel 实现本类型在当前服务边界中的行为。
func (u *User) NewModel() {
	if u.BaseDataModel == nil || u.UserServiceModel == nil || u.Model == nil {
		u.BaseDataModel = common.NewBaseDataModel()
	}
}

// GetHash 实现本类型在当前服务边界中的行为。
func (u *User) GetHash() string { return utils.HashCodes(strings.TrimSpace(u.AuthUserID)) }
