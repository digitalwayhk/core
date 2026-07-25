package dto

import "github.com/digitalwayhk/core/examples/02-shop-payment/models"

// PaymentTypeResponse 是用户可选择的启用支付类型 DTO。
type PaymentTypeResponse struct {
	ID          uint   `json:"id,string" desc:"支付类型 ID"`
	Code        string `json:"code" desc:"支付类型编码"`
	Name        string `json:"name" desc:"支付类型名称"`
	Description string `json:"description" desc:"支付类型说明"`
}

// PaymentTypeResponses 转换启用支付类型列表且不暴露内部模型字段。
func PaymentTypeResponses(items []*models.PaymentType) []*PaymentTypeResponse {
	result := make([]*PaymentTypeResponse, 0, len(items))
	for _, item := range items {
		if item != nil {
			result = append(result, &PaymentTypeResponse{ID: item.ID, Code: item.Code, Name: item.Name, Description: item.Description})
		}
	}
	return result
}
