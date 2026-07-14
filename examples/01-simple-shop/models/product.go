package models

import (
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
)

// Product 表示商城中可下单的商品。
type Product struct {
	*entity.Model
	Name   string          `json:"name" desc:"商品名称"`
	Price  decimal.Decimal `json:"price" desc:"商品价格"`
	action persistencetypes.IDataAction
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

// SetDataAction 为需要执行唯一性校验的商品注入数据适配器。
// Manage hook 会在调用 AddValid/UpdateValid 前设置它；该字段不会持久化或序列化。
func (own *Product) SetDataAction(action persistencetypes.IDataAction) {
	own.action = action
}

// GetHash 以去除首尾空白的商品名称生成唯一哈希。
func (own *Product) GetHash() string {
	name := strings.TrimSpace(own.Name)
	if name == "" {
		return ""
	}
	return utils.HashCodes(name)
}

// AddValid 在 Manage 新增商品前验证名称、价格和名称唯一性。
func (own *Product) AddValid() error {
	return own.validate(true)
}

// UpdateValid 在 Manage 修改商品前验证名称、价格和名称唯一性。
func (own *Product) UpdateValid(_ interface{}) error {
	return own.validate(true)
}

// validate 统一商品新增与修改的业务约束。
func (own *Product) validate(checkDuplicate bool) error {
	own.Name = strings.TrimSpace(own.Name)
	if own.Name == "" {
		return NewValidationError("商品名称不能为空")
	}
	if !own.Price.GreaterThan(decimal.Zero) {
		return NewValidationError("商品价格必须大于 0")
	}
	if checkDuplicate {
		exists, err := own.NameExists(own.action, own.Name, own.ID)
		if err != nil {
			return err
		}
		if exists {
			return NewBusinessError("商品名称不能重复")
		}
	}
	return nil
}
