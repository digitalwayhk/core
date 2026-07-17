package common

import "github.com/digitalwayhk/core/pkg/persistence/entity"

const DatabaseName = "shop-supplier"

type SupplierServiceModel struct {
	*entity.Model
}

func NewSupplierServiceModel() *SupplierServiceModel {
	return &SupplierServiceModel{Model: entity.NewModel()}
}

func (m *SupplierServiceModel) GetLocalDBName() string  { return DatabaseName }
func (m *SupplierServiceModel) GetRemoteDBName() string { return DatabaseName }
