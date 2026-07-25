package models

import (
	"strings"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/utils"
)

// PaymentType 表示用户可选择的支付方式，不保存第三方密钥。
type PaymentType struct {
	*entity.Model
	Code        string `json:"code" desc:"支付类型编码"`
	Name        string `json:"name" desc:"支付类型名称"`
	Enabled     bool   `json:"enabled" desc:"是否启用"`
	Description string `json:"description" desc:"支付类型说明"`
}

// NewPaymentType 创建已初始化基础模型的支付类型。
func NewPaymentType() *PaymentType {
	return &PaymentType{Model: entity.NewModel()}
}

// NewModel 供 ModelList 反射创建支付类型时初始化基础模型。
func (own *PaymentType) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}

// Normalize 规范化稳定编码和展示文字。
func (own *PaymentType) Normalize() error {
	own.Code = strings.ToLower(strings.TrimSpace(own.Code))
	own.Name = strings.TrimSpace(own.Name)
	own.Description = strings.TrimSpace(own.Description)
	if own.Code == "" {
		return NewValidationError("支付类型编码不能为空")
	}
	if own.Name == "" {
		return NewValidationError("支付类型名称不能为空")
	}
	return nil
}

// GetHash 以规范化支付类型编码生成唯一哈希。
func (own *PaymentType) GetHash() string {
	code := strings.ToLower(strings.TrimSpace(own.Code))
	if code == "" {
		return ""
	}
	return utils.HashCodes(code)
}
