package models

import "github.com/digitalwayhk/core/pkg/persistence/entity"

// Product 使用 BaseModel，适合可提交/发布的基础资料。
type Product struct {
	*entity.BaseModel
	Price int64 `json:"price" desc:"价格，单位分"`
}

// NewProduct 手动创建商品模型。
func NewProduct() *Product {
	return &Product{BaseModel: entity.NewBaseModel()}
}

// NewModel 供 ModelList 创建新对象时初始化 BaseModel。
func (own *Product) NewModel() {
	if own.BaseModel == nil {
		own.BaseModel = entity.NewBaseModel()
	}
}
