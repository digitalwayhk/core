// 本文件定义当前服务基础资料模型及其持久化能力。
package basedata

import (
	"strconv"
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models/common"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
)

// Product 定义本文件能力使用的核心结构。
type Product struct {
	*common.BaseDataModel
	SupplierID uint            `gorm:"not null;index:idx_product_supplier_code,unique" json:"supplierID"`
	Name       string          `gorm:"not null" json:"name"`
	Code       string          `gorm:"not null;index:idx_product_supplier_code,unique" json:"code"`
	Price      decimal.Decimal `json:"price"`
	Enabled    bool            `json:"enabled"`
}

// NewProduct 执行本文件能力对应的业务操作。
func NewProduct() *Product { return &Product{BaseDataModel: common.NewBaseDataModel()} }

// NewModel 实现本类型在当前服务边界中的行为。
func (p *Product) NewModel() {
	if p.BaseDataModel == nil || p.SupplierServiceModel == nil || p.Model == nil {
		p.BaseDataModel = common.NewBaseDataModel()
	}
}

// GetHash 实现本类型在当前服务边界中的行为。
func (p *Product) GetHash() string {
	return utils.HashCodes(strconv.FormatUint(uint64(p.SupplierID), 10), strings.ToLower(strings.TrimSpace(p.Code)))
}
