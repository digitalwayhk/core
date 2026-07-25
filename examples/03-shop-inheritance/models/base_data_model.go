package models

import (
	"strings"

	"github.com/digitalwayhk/core/pkg/utils"
)

// IBaseDataModel 供通用 Manage 在不使用反射的情况下访问基础资料字段。
type IBaseDataModel interface {
	GetBaseDataModel() *BaseDataModel
}

// BaseDataModel 是商品、供应商和支付类型共享的基础资料模型。
type BaseDataModel struct {
	*ShopModel
	Code        string `gorm:"not null;uniqueIndex" json:"code" desc:"编码"`
	Name        string `gorm:"not null;uniqueIndex" json:"name" desc:"名称"`
	Enabled     bool   `json:"enabled" desc:"是否启用"`
	Description string `json:"description" desc:"说明"`
}

// NewBaseDataModel 创建默认禁用的基础资料模型。
func NewBaseDataModel() *BaseDataModel {
	return &BaseDataModel{ShopModel: NewShopModel()}
}

// GetBaseDataModel 返回继承链中的基础资料模型。
func (own *BaseDataModel) GetBaseDataModel() *BaseDataModel { return own }

// NormalizeBaseData 统一规范化基础资料字段。
func (own *BaseDataModel) NormalizeBaseData() error {
	own.Code = strings.ToLower(strings.TrimSpace(own.Code))
	own.Name = strings.TrimSpace(own.Name)
	own.Description = strings.TrimSpace(own.Description)
	if own.Code == "" {
		return NewValidationError("编码不能为空")
	}
	if own.Name == "" {
		return NewValidationError("名称不能为空")
	}
	return nil
}

// GetHash 使用规范化后的稳定编码生成模型哈希。
func (own *BaseDataModel) GetHash() string {
	code := strings.ToLower(strings.TrimSpace(own.Code))
	if code == "" {
		if own.ShopModel != nil && own.Model != nil {
			return own.Hashcode
		}
		return ""
	}
	return utils.HashCodes(code)
}
