package dto

import "github.com/digitalwayhk/core/examples/03-shop-inheritance/models"

// SupplierResponse 是 Public API 对外暴露的供应商 DTO。
type SupplierResponse struct {
	ID          uint   `json:"id,string" desc:"供应商 ID"`
	Code        string `json:"code" desc:"供应商编码"`
	Name        string `json:"name" desc:"供应商名称"`
	Description string `json:"description" desc:"供应商说明"`
}

// SupplierResponses 将供应商模型转换为公开 DTO。
func SupplierResponses(items []*models.Supplier) []*SupplierResponse {
	result := make([]*SupplierResponse, 0, len(items))
	for _, item := range items {
		if item != nil {
			result = append(result, &SupplierResponse{ID: item.ID, Code: item.Code, Name: item.Name, Description: item.Description})
		}
	}
	return result
}
