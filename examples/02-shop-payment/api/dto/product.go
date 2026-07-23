package dto

import "github.com/digitalwayhk/core/examples/02-shop-payment/models"

// ProductResponse 是 Public API 对外暴露的商品 DTO。
type ProductResponse struct {
	ID    uint   `json:"id,string" desc:"商品 ID"`
	Name  string `json:"name" desc:"商品名称"`
	Price string `json:"price" desc:"商品价格"`
}

// NewProductResponse 从商品持久化模型创建公开 DTO。
func NewProductResponse(model *models.Product) *ProductResponse {
	if model == nil {
		return nil
	}
	return &ProductResponse{ID: model.ID, Name: model.Name, Price: model.Price.String()}
}

// ProductResponses 转换商品列表。
func ProductResponses(items []*models.Product) []*ProductResponse {
	result := make([]*ProductResponse, 0, len(items))
	for _, item := range items {
		if response := NewProductResponse(item); response != nil {
			result = append(result, response)
		}
	}
	return result
}
