package basedata

import (
	"strconv"
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models/common"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
)

type Product struct {
	*common.BaseDataModel
	SupplierID uint            `gorm:"not null;index:idx_product_supplier_code,unique" json:"supplierID"`
	Name       string          `gorm:"not null" json:"name"`
	Code       string          `gorm:"not null;index:idx_product_supplier_code,unique" json:"code"`
	Price      decimal.Decimal `json:"price"`
	Enabled    bool            `json:"enabled"`
}

func NewProduct() *Product { return &Product{BaseDataModel: common.NewBaseDataModel()} }

func (p *Product) NewModel() {
	if p.BaseDataModel == nil || p.SupplierServiceModel == nil || p.Model == nil {
		p.BaseDataModel = common.NewBaseDataModel()
	}
}

func (p *Product) GetHash() string {
	return utils.HashCodes(strconv.FormatUint(uint64(p.SupplierID), 10), strings.ToLower(strings.TrimSpace(p.Code)))
}
