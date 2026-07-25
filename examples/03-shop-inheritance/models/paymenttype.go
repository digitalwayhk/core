package models

// PaymentType 表示用户可选择的支付方式。
type PaymentType struct {
	*BaseDataModel
}

// NewPaymentType 创建完整初始化且默认禁用的支付类型。
func NewPaymentType() *PaymentType { return &PaymentType{BaseDataModel: NewBaseDataModel()} }

// NewModel 供 ModelList 反射创建支付类型时初始化完整继承链。
func (own *PaymentType) NewModel() {
	if own.BaseDataModel == nil || own.ShopModel == nil || own.Model == nil {
		own.BaseDataModel = NewBaseDataModel()
	}
}

// Normalize 兼容支付业务对基础资料规范化的调用。
func (own *PaymentType) Normalize() error { return own.NormalizeBaseData() }

// AddValid 校验新增支付类型的公共字段和唯一性。
func (own *PaymentType) AddValid() error { return own.validate(0) }

// UpdateValid 校验修改支付类型并排除当前记录。
func (own *PaymentType) UpdateValid(interface{}) error { return own.validate(own.ID) }

// RemoveValid 的流水引用保护由业务层和 PaymentTypeManage 执行。
func (own *PaymentType) RemoveValid() error { return nil }

func (own *PaymentType) validate(excludeID uint) error {
	if err := own.NormalizeBaseData(); err != nil {
		return err
	}
	exists, err := own.CodeOrNameExists(own.Code, own.Name, excludeID)
	if err != nil {
		return err
	}
	if exists {
		return NewBusinessError("支付类型编码或名称不能重复")
	}
	return nil
}
