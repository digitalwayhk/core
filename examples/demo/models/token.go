package models

import "github.com/digitalwayhk/core/pkg/persistence/entity"

// TokenModel 币种模型
type TokenModel struct {
	*entity.Model
	Name   string
	Orders []*OrderModel `gorm:"foreignkey:TokenID"`
}

// NewTokenModel 新建币种模型
func NewTokenModel() *TokenModel {
	return &TokenModel{
		Model: entity.NewModel(),
	}
}

// NewTokenModel 新建币种模型，用于ModelList的NewItem方法
func (own *TokenModel) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}
