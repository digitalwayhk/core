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
	Supplier   *Supplier       `gorm:"-" json:"supplier,omitempty"`
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

// AddValid 校验 Manage 新增商品所需的完整业务字段。
func (p *Product) AddValid() error { return p.validate() }

// UpdateValid 校验 Manage 编辑商品所需的完整业务字段。
func (p *Product) UpdateValid(interface{}) error { return p.validate() }

func (p *Product) validate() error {
	if p.SupplierID == 0 {
		return errors.New("商品供应商不能为空")
	}
	if strings.TrimSpace(p.Code) == "" || strings.TrimSpace(p.Name) == "" || !p.Price.IsPositive() {
		return errors.New("商品名称、编码和正数价格不能为空")
	}
	return nil
}

// InsertWith 将商品写入指定事务。
func (p *Product) InsertWith(action persistencetypes.IDataAction) error {
	if err := p.validate(); err != nil {
		return err
	}
	p.SetHashcode(p.GetHash())
	return action.Insert(p)
}

// UpdateWith 更新指定事务中的商品。
func (p *Product) UpdateWith(action persistencetypes.IDataAction) error {
	if err := p.validate(); err != nil {
		return err
	}
	p.SetUpdatedAt(time.Now().UTC())
	p.SetHashcode(p.GetHash())
	return action.Update(p)
}
