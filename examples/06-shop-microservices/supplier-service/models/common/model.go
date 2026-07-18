package common

import "github.com/digitalwayhk/core/pkg/persistence/entity"

const DatabaseName = "shop-supplier"

type SupplierServiceModel struct {
	*entity.Model
	TraceID string `gorm:"index" json:"traceID"`
}

func NewSupplierServiceModel() *SupplierServiceModel {
	return &SupplierServiceModel{Model: entity.NewModel()}
}

func (m *SupplierServiceModel) GetLocalDBName() string  { return DatabaseName }
func (m *SupplierServiceModel) GetRemoteDBName() string { return DatabaseName }
