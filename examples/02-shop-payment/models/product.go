package models

import (
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
)

// Product 表示支付商城中可下单的商品。
type Product struct {
	*entity.Model
	Name  string          `json:"name" desc:"商品名称"`
	Price decimal.Decimal `json:"price" desc:"商品价格"`
}

// NewProduct 创建已初始化基础模型的商品。
func NewProduct() *Product { return &Product{Model: entity.NewModel()} }

// NewModel 供 ModelList 反射创建商品时初始化基础模型。
func (own *Product) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}

// GetHash 以规范化商品名称生成唯一哈希。
func (own *Product) GetHash() string {
	name := strings.TrimSpace(own.Name)
	if name == "" {
		return ""
	}
	return utils.HashCodes(name)
}

// AddValid 校验新增商品。
func (own *Product) AddValid() error { return own.validate() }

// UpdateValid 校验修改商品。
func (own *Product) UpdateValid(interface{}) error { return own.validate() }

// validate 统一商品字段与名称唯一性校验。
func (own *Product) validate() error {
	own.Name = strings.TrimSpace(own.Name)
	if own.Name == "" {
		return NewValidationError("商品名称不能为空")
	}
	if !own.Price.GreaterThan(decimal.Zero) {
		return NewValidationError("商品价格必须大于 0")
	}
	exists, err := own.NameExists(own.Name, own.ID)
	if err != nil {
		return err
	}
	if exists {
		return NewBusinessError("商品名称不能重复")
	}
	return nil
}
