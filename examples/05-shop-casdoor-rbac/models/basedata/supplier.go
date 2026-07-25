package basedata

import "github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models/common"

// Supplier 表示商品所属供应商。
type Supplier struct {
	*common.BaseDataModel
	Products []*Product `gorm:"foreignKey:SupplierID" json:"products,omitempty" desc:"商品集合"`
}

// NewSupplier 创建完整初始化且默认禁用的供应商。
func NewSupplier() *Supplier { return &Supplier{BaseDataModel: common.NewBaseDataModel()} }

// NewModel 供 ModelList 反射创建供应商时初始化完整继承链。
func (own *Supplier) NewModel() {
	if own.BaseDataModel == nil || own.ShopModel == nil || own.Model == nil {
		own.BaseDataModel = common.NewBaseDataModel()
	}
}

// AddValid 校验新增供应商的公共字段和唯一性。
func (own *Supplier) AddValid() error { return own.validate(0) }

// UpdateValid 校验修改供应商并排除当前记录。
func (own *Supplier) UpdateValid(interface{}) error { return own.validate(own.ID) }

// RemoveValid 阻止删除仍有商品引用的供应商；有引用时只能禁用。
func (own *Supplier) RemoveValid() error {
	used, err := own.HasProducts(own.ID)
	if err != nil {
		return err
	}
	if used {
		return common.NewBusinessError("供应商已有商品使用，只能禁用")
	}
	return nil
}

func (own *Supplier) validate(excludeID uint) error {
	if err := own.NormalizeBaseData(); err != nil {
		return err
	}
	exists, err := own.CodeOrNameExists(own.Code, own.Name, excludeID)
	if err != nil {
		return err
	}
	if exists {
		return common.NewBusinessError("供应商编码或名称不能重复")
	}
	return nil
}
