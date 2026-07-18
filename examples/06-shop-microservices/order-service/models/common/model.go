package common

import "github.com/digitalwayhk/core/pkg/persistence/entity"

const DatabaseName = "shop-order"

type OrderServiceModel struct {
	*entity.Model
	TraceID string `gorm:"index" json:"traceID"`
}

func NewOrderServiceModel() *OrderServiceModel {
	return &OrderServiceModel{Model: entity.NewModel()}
}

func (m *OrderServiceModel) GetLocalDBName() string  { return DatabaseName }
func (m *OrderServiceModel) GetRemoteDBName() string { return DatabaseName }
