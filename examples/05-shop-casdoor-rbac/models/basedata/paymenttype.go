package basedata

import (
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models/common"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models/transaction"
)

// PaymentType 表示用户可选择的支付方式。
type PaymentType struct {
	*common.BaseDataModel
}

// NewPaymentType 创建完整初始化且默认禁用的支付类型。
func NewPaymentType() *PaymentType { return &PaymentType{BaseDataModel: common.NewBaseDataModel()} }

// NewModel 供 ModelList 反射创建支付类型时初始化完整继承链。
func (own *PaymentType) NewModel() {
	if own.BaseDataModel == nil || own.ShopModel == nil || own.Model == nil {
		own.BaseDataModel = common.NewBaseDataModel()
	}
}

// Normalize 兼容支付业务对基础资料规范化的调用。
func (own *PaymentType) Normalize() error { return own.NormalizeBaseData() }

// AddValid 校验新增支付类型的公共字段和唯一性。
func (own *PaymentType) AddValid() error { return own.validate(0) }

// UpdateValid 校验修改支付类型并排除当前记录。
func (own *PaymentType) UpdateValid(interface{}) error { return own.validate(own.ID) }

// RemoveValid 阻止删除已被支付流水引用的支付类型；有引用时只能禁用。
func (own *PaymentType) RemoveValid() error {
	used, err := transaction.NewPaymentRecord().ExistsByPaymentTypeID(own.ID)
	if err != nil {
		return err
	}
	if used {
		return common.NewBusinessError("支付类型已被支付流水使用，只能禁用")
	}
	return nil
}

func (own *PaymentType) validate(excludeID uint) error {
	if err := own.NormalizeBaseData(); err != nil {
		return err
	}
	exists, err := own.CodeOrNameExists(own.Code, own.Name, excludeID)
	if err != nil {
		return err
	}
	if exists {
		return common.NewBusinessError("支付类型编码或名称不能重复")
	}
	return nil
}
