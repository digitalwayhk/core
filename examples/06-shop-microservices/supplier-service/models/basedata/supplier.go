// 本文件定义当前服务基础资料模型及其持久化能力。
package basedata

import (
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models/common"
	"github.com/digitalwayhk/core/pkg/utils"
)

// Supplier 定义本文件能力使用的核心结构。
type Supplier struct {
	*common.BaseDataModel
	AuthUserID  string `gorm:"not null;uniqueIndex" json:"-"`
	Name        string `gorm:"not null;uniqueIndex" json:"name"`
	Code        string `gorm:"not null;uniqueIndex" json:"code"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

// NewSupplier 执行本文件能力对应的业务操作。
func NewSupplier() *Supplier { return &Supplier{BaseDataModel: common.NewBaseDataModel()} }

// NewModel 实现本类型在当前服务边界中的行为。
func (s *Supplier) NewModel() {
	if s.BaseDataModel == nil || s.SupplierServiceModel == nil || s.Model == nil {
		s.BaseDataModel = common.NewBaseDataModel()
	}
}

// GetHash 实现本类型在当前服务边界中的行为。
func (s *Supplier) GetHash() string {
	return utils.HashCodes(strings.ToLower(strings.TrimSpace(s.AuthUserID)))
}
