// Package basedata 定义 07 供应商服务商品基础资料模型。
package basedata

import (
	"errors"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/models/common"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
)

// Product 保存供应商维护的商品资料。
type Product struct {
	*common.ServiceBaseModel
	SupplierID uint            `gorm:"not null;index" json:"supplierID"`
	Code       string          `gorm:"not null;uniqueIndex" json:"code"`
	Name       string          `gorm:"not null" json:"name"`
	Price      decimal.Decimal `json:"price"`
	Enabled    bool            `gorm:"index" json:"enabled"`
}

// NewProduct 创建商品模型。
func NewProduct() *Product {
	return &Product{ServiceBaseModel: common.NewServiceBaseModel(), Enabled: true}
}

// NewModel 初始化持久化框架需要的嵌入模型。
func (p *Product) NewModel() {
	if p.ServiceBaseModel == nil || p.Model == nil {
		p.ServiceBaseModel = common.NewServiceBaseModel()
	}
}

// GetHash 返回商品业务唯一散列。
func (p *Product) GetHash() string { return utils.HashCodes(strings.TrimSpace(p.Code)) }

// InsertWith 将商品写入指定事务。
func (p *Product) InsertWith(action persistencetypes.IDataAction) error {
	if p.SupplierID == 0 || strings.TrimSpace(p.Code) == "" || strings.TrimSpace(p.Name) == "" || !p.Price.IsPositive() {
		return errors.New("商品参数不完整")
	}
	p.SetHashcode(p.GetHash())
	return action.Insert(p)
}

// UpdateWith 更新指定事务中的商品。
func (p *Product) UpdateWith(action persistencetypes.IDataAction) error {
	p.SetUpdatedAt(time.Now().UTC())
	p.SetHashcode(p.GetHash())
	return action.Update(p)
}
