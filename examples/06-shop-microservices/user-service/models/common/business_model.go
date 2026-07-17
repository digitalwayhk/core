package common

type BusinessModel struct {
	*UserServiceModel
}

func NewBusinessModel() *BusinessModel {
	return &BusinessModel{UserServiceModel: NewUserServiceModel()}
}
