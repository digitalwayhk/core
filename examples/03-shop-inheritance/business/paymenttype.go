package business

import "github.com/digitalwayhk/core/examples/03-shop-inheritance/models"

// PaymentTypeService 处理支付类型查询、校验和启停。
type PaymentTypeService struct{}

// NewPaymentTypeService 创建无状态支付类型业务服务。
func NewPaymentTypeService() *PaymentTypeService { return &PaymentTypeService{} }

// ListEnabled 返回可供用户选择的支付类型。
func (own *PaymentTypeService) ListEnabled(code, name string) ([]*models.PaymentType, error) {
	return models.NewPaymentType().QueryEnabled(code, name)
}

// ValidateCreate 校验支付类型规范和唯一性。
func (own *PaymentTypeService) ValidateCreate(item *models.PaymentType) error {
	if err := item.Normalize(); err != nil {
		return err
	}
	exists, err := item.CodeOrNameExists(item.Code, item.Name, item.ID)
	if err != nil {
		return err
	}
	if exists {
		return models.NewBusinessError("支付类型编码或名称不能重复")
	}
	return nil
}

// ValidateUpdate 校验支付类型编辑约束。
func (own *PaymentTypeService) ValidateUpdate(item, old *models.PaymentType) error {
	if item == nil || old == nil {
		return models.NewBusinessError("支付类型不存在")
	}
	if err := own.ValidateCreate(item); err != nil {
		return err
	}
	if item.Enabled != old.Enabled {
		return models.NewBusinessError("启用状态只能通过启用或禁用按钮修改")
	}
	used, err := models.NewPaymentRecord().ExistsByPaymentTypeID(old.ID)
	if err != nil {
		return err
	}
	if used && item.Code != old.Code {
		return models.NewBusinessError("已使用的支付类型编码不能修改")
	}
	return nil
}

// EnsureRemovable 阻止删除已被支付流水引用的支付类型。
func (own *PaymentTypeService) EnsureRemovable(paymentTypeID uint) error {
	used, err := models.NewPaymentRecord().ExistsByPaymentTypeID(paymentTypeID)
	if err != nil {
		return err
	}
	if used {
		return models.NewBusinessError("支付类型已被支付流水使用，只能禁用")
	}
	return nil
}

// Enable 启用支付类型并保持重复操作幂等。
func (own *PaymentTypeService) Enable(id uint) (*models.PaymentType, error) {
	return own.SetEnabled(id, true)
}

// Disable 禁用支付类型但不影响已有流水。
func (own *PaymentTypeService) Disable(id uint) (*models.PaymentType, error) {
	return own.SetEnabled(id, false)
}

// SetEnabled 统一启用和禁用的数据更新逻辑。
func (own *PaymentTypeService) SetEnabled(id uint, enabled bool) (*models.PaymentType, error) {
	item, err := models.NewPaymentType().FindByID(id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, models.NewBusinessError("支付类型不存在")
	}
	if item.Enabled == enabled {
		return item, nil
	}
	item.Enabled = enabled
	if err := item.Update(); err != nil {
		return nil, err
	}
	return item, nil
}
