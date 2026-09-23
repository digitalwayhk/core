package dto

import "github.com/digitalwayhk/core/examples/08-admin-manage-ui/models"

// CategoryResponse 是 Public API 对外暴露的最小分类 DTO。
type CategoryResponse struct {
	ID      uint   `json:"id,string" desc:"分类 ID"`
	Code    string `json:"code" desc:"分类编码"`
	Name    string `json:"name" desc:"分类名称"`
	Kind    int    `json:"kind" desc:"分类类型"`
	Enabled bool   `json:"enabled" desc:"是否启用"`
}

// NewCategoryResponse 从持久化分类创建不含基础模型字段的响应 DTO。
func NewCategoryResponse(model *models.Category) *CategoryResponse {
	if model == nil {
		return nil
	}
	return &CategoryResponse{
		ID:      model.ID,
		Code:    model.Code,
		Name:    model.Name,
		Kind:    model.Kind,
		Enabled: model.Enabled,
	}
}

// CategoryResponses 将分类持久化列表转换为对外响应 DTO 列表。
func CategoryResponses(items []*models.Category) []*CategoryResponse {
	result := make([]*CategoryResponse, 0, len(items))
	for _, item := range items {
		if response := NewCategoryResponse(item); response != nil {
			result = append(result, response)
		}
	}
	return result
}
