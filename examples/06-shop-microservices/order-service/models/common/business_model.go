package common

type BusinessModel struct {
	*OrderServiceModel
}

func NewBusinessModel() *BusinessModel {
	return &BusinessModel{OrderServiceModel: NewOrderServiceModel()}
}
