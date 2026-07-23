package models

// BusinessModel 是订单和支付流水共享的业务数据模型。
type BusinessModel struct {
	*ShopModel
	Status int `json:"status" desc:"业务状态"`
}

// NewBusinessModel 创建业务数据基础模型。
func NewBusinessModel(status int) *BusinessModel {
	return &BusinessModel{ShopModel: NewShopModel(), Status: status}
}

// GetBusinessModel 返回继承链中的业务数据模型。
func (own *BusinessModel) GetBusinessModel() *BusinessModel { return own }
