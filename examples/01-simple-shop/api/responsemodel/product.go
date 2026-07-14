package responsemodel

import "github.com/digitalwayhk/core/examples/01-simple-shop/models"

// Product 是 Public API 对外暴露的最小商品响应模型。
type Product struct {
	ID    uint   `json:"id,string" desc:"商品 ID"`
	Name  string `json:"name" desc:"商品名称"`
	Price string `json:"price" desc:"商品价格"`
}

// NewProduct 从持久化商品创建不含基础模型字段的响应快照。
func NewProduct(model *models.Product) *Product {
	if model == nil {
		return nil
	}
	return &Product{ID: model.ID, Name: model.Name, Price: model.Price.String()}
}

// Products 将商品持久化列表转换为对外响应列表。
func Products(modelsList []*models.Product) []*Product {
	result := make([]*Product, 0, len(modelsList))
	for _, model := range modelsList {
		if response := NewProduct(model); response != nil {
			result = append(result, response)
		}
	}
	return result
}
