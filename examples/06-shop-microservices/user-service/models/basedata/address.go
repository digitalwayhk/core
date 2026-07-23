// 本文件定义当前服务基础资料模型及其持久化能力。
package basedata

import (
	"strconv"
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models/common"
	"github.com/digitalwayhk/core/pkg/utils"
)

// Address 定义本文件能力使用的核心结构。
type Address struct {
	*common.BaseDataModel
	UserID    uint   `gorm:"not null;index" json:"userID"`
	Recipient string `gorm:"not null" json:"recipient"`
	Phone     string `json:"phone"`
	Region    string `json:"region"`
	Detail    string `json:"detail"`
}

// NewAddress 执行本文件能力对应的业务操作。
func NewAddress() *Address { return &Address{BaseDataModel: common.NewBaseDataModel()} }

// NewModel 实现本类型在当前服务边界中的行为。
func (a *Address) NewModel() {
	if a.BaseDataModel == nil || a.UserServiceModel == nil || a.Model == nil {
		a.BaseDataModel = common.NewBaseDataModel()
	}
}

// GetHash 实现本类型在当前服务边界中的行为。
func (a *Address) GetHash() string {
	return utils.HashCodes(strconv.FormatUint(uint64(a.UserID), 10), strings.TrimSpace(a.Recipient), strings.TrimSpace(a.Phone), strings.TrimSpace(a.Detail))
}
