package models

import (
	"strconv"
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
)

// Product 表示商城中可下单的商品。
type Product struct {
	*entity.Model
	Name  string          `json:"name" desc:"商品名称"`
	Price decimal.Decimal `json:"price" desc:"商品价格"`
}

// NewProduct 创建已初始化基础模型的商品。
func NewProduct() *Product {
	return &Product{Model: entity.NewModel()}
}

// NewModel 供 ModelList 在反射创建商品时初始化基础模型。
func (own *Product) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}

// GetHash 优先返回已保存的 Hashcode，并为具有 ID 的新商品生成稳定值。
func (own *Product) GetHash() string {
	if own.Model == nil {
		return ""
	}
	if own.Hashcode != "" {
		return own.Hashcode
	}
	if own.ID == 0 {
		return ""
	}
	return utils.HashCodes(strconv.FormatUint(uint64(own.ID), 10))
}

// AddValid 在 Manage 新增商品前验证名称和价格。
func (own *Product) AddValid() error {
	return own.validate()
}

// UpdateValid 在 Manage 修改商品前验证名称和价格。
func (own *Product) UpdateValid(_ interface{}) error {
	return own.validate()
}

// validate 统一商品新增与修改的业务约束。
func (own *Product) validate() error {
	own.Name = strings.TrimSpace(own.Name)
	if own.Name == "" {
		return NewValidationError("商品名称不能为空")
	}
	if !own.Price.GreaterThan(decimal.Zero) {
		return NewValidationError("商品价格必须大于 0")
	}
	return nil
}
