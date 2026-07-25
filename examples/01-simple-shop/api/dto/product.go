package dto

import "github.com/digitalwayhk/core/examples/01-simple-shop/models"

// ProductResponse 是 Public API 对外暴露的最小商品 DTO。
type ProductResponse struct {
	ID    uint   `json:"id,string" desc:"商品 ID"`
	Name  string `json:"name" desc:"商品名称"`
	Price string `json:"price" desc:"商品价格"`
}

// NewProductResponse 从持久化商品创建不含基础模型字段的响应 DTO。
func NewProductResponse(model *models.Product) *ProductResponse {
	if model == nil {
		return nil
	}
	return &ProductResponse{ID: model.ID, Name: model.Name, Price: model.Price.String()}
}

// ProductResponses 将商品持久化列表转换为对外响应 DTO 列表。
func ProductResponses(modelsList []*models.Product) []*ProductResponse {
	result := make([]*ProductResponse, 0, len(modelsList))
	for _, model := range modelsList {
		if response := NewProductResponse(model); response != nil {
			result = append(result, response)
		}
	}
	return result
}
