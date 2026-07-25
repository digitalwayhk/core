package basedata

import (
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models/common"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models/transaction"
	"github.com/shopspring/decimal"
)

// Product 表示由供应商提供、可供用户下单的商品。
type Product struct {
	*common.BaseDataModel
	SupplierID uint            `gorm:"not null;index" json:"supplierID" desc:"供应商 ID"`
	Price      decimal.Decimal `json:"price" desc:"商品价格"`
	Supplier   *Supplier       `gorm:"-" json:"-"`
}

// NewProduct 创建完整初始化且默认禁用的商品。
func NewProduct() *Product { return &Product{BaseDataModel: common.NewBaseDataModel()} }

// NewModel 供 ModelList 反射创建商品时初始化完整继承链。
func (own *Product) NewModel() {
	if own.BaseDataModel == nil || own.ShopModel == nil || own.Model == nil {
		own.BaseDataModel = common.NewBaseDataModel()
	}
}

// AddValid 校验新增商品的公共字段、价格、供应商和唯一性。
func (own *Product) AddValid() error { return own.validate(0) }

// UpdateValid 校验修改商品并排除当前记录。
func (own *Product) UpdateValid(interface{}) error { return own.validate(own.ID) }

// RemoveValid 阻止删除已被历史订单引用的商品；有引用时只能禁用。
func (own *Product) RemoveValid() error {
	used, err := transaction.NewOrder().ExistsByProductID(own.ID)
	if err != nil {
		return err
	}
	if used {
		return common.NewBusinessError("商品已被订单使用，只能禁用")
	}
	return nil
}

func (own *Product) validate(excludeID uint) error {
	if err := own.NormalizeBaseData(); err != nil {
		return err
	}
	if own.SupplierID == 0 {
		return common.NewValidationError("请选择供应商")
	}
	if !own.Price.GreaterThan(decimal.Zero) {
		return common.NewValidationError("商品价格必须大于 0")
	}
	exists, err := own.CodeOrNameExists(own.Code, own.Name, excludeID)
	if err != nil {
		return err
	}
	if exists {
		return common.NewBusinessError("商品编码或名称不能重复")
	}
	return nil
}
