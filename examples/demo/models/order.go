package models

import (
	"github.com/digitalwayhk/core/pkg/persistence/entity"

	"github.com/shopspring/decimal"
)

// OrderModel 订单模型
type OrderModel struct {
	*entity.Model                 //从基础Model继承，默认添加ID,创建时间和状态字段
	UserID        string          //用户ID
	Price         decimal.Decimal //价格
	Amount        decimal.Decimal //数量
	TokenID       uint            //币种ID
	ParnetID      uint            //父订单ID
	ChildDetail   []*OrderModel   `gorm:"foreignkey:ParnetID"` //子订单
}

// NewOrderModel 新建订单模型
func NewOrderModel() *OrderModel {
	return &OrderModel{
		Model: entity.NewModel(),
	}
}

// NewOrderModel 新建订单模型，用于ModelList的NewItem方法
func (own *OrderModel) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}

// func (own *OrderModel) SearchSQL() string {
// 	return "select * from OrderModel"
// }
