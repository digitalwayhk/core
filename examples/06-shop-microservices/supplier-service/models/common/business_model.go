package common

type BusinessModel struct {
	*SupplierServiceModel
}

func NewBusinessModel() *BusinessModel {
	return &BusinessModel{SupplierServiceModel: NewSupplierServiceModel()}
}
