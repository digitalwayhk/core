package dto

import "github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models"

// ProductResponse 是 Public API 对外暴露的商品 DTO。
type ProductResponse struct {
	ID           uint   `json:"id,string" desc:"商品 ID"`
	Code         string `json:"code" desc:"商品编码"`
	Name         string `json:"name" desc:"商品名称"`
	Price        string `json:"price" desc:"商品价格"`
	SupplierID   uint   `json:"supplierID" desc:"供应商 ID"`
	SupplierCode string `json:"supplierCode" desc:"供应商编码"`
	SupplierName string `json:"supplierName" desc:"供应商名称"`
}

// NewProductResponse 从商品持久化模型创建公开 DTO。
func NewProductResponse(model *models.Product) *ProductResponse {
	if model == nil {
		return nil
	}
	response := &ProductResponse{ID: model.ID, Code: model.Code, Name: model.Name, Price: model.Price.String(), SupplierID: model.SupplierID}
	if model.Supplier != nil {
		response.SupplierCode = model.Supplier.Code
		response.SupplierName = model.Supplier.Name
	}
	return response
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
