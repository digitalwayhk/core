package common

type BaseDataModel struct {
	*SupplierServiceModel
}

func NewBaseDataModel() *BaseDataModel {
	return &BaseDataModel{SupplierServiceModel: NewSupplierServiceModel()}
}
