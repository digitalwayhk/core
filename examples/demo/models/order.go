package models

import (
	"github.com/digitalwayhk/core/pkg/persistence/entity"

	"github.com/shopspring/decimal"
)

// OrderModel 订单模型。
// 使用 *entity.BaseModel 作为基础，提供 ID、State、Code、Name 等标准业务字段。
type OrderModel struct {
	*entity.BaseModel                 // 基础业务模型：含 ID、State（0=待提交 1=已提交 2=已发布）等
	UserID            string          // 用户ID
	Price             decimal.Decimal // 成交价格
	Amount            decimal.Decimal // 成交数量
	TokenID           uint            // 关联币种ID
	ParnetID          uint            // 父订单ID
	ChildDetail       []*OrderModel   `gorm:"foreignkey:ParnetID"` // 子订单
}

// NewOrderModel 新建订单模型
func NewOrderModel() *OrderModel {
	return &OrderModel{
		BaseModel: entity.NewBaseModel(),
	}
}

// NewModel 新建订单模型，用于 ModelList 的 NewItem 方法
func (own *OrderModel) NewModel() {
	if own.BaseModel == nil {
		own.BaseModel = entity.NewBaseModel()
	}
}

// GetHash 使用 ID 而非 Code 作为哈希依据。
// 订单没有业务 Code，保持与 entity.Model 相同的 ID 哈希策略，避免空 Code 导致哈希碰撞。
func (own *OrderModel) GetHash() string {
	if own.BaseModel != nil && own.BaseModel.Model != nil {
		return own.BaseModel.Model.GetHash()
	}
	return own.BaseModel.GetHash()
}

// func (own *OrderModel) SearchSQL() string {
// 	return "select * from OrderModel"
// }
