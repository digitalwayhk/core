package common

type BaseDataModel struct {
	*UserServiceModel
}

func NewBaseDataModel() *BaseDataModel {
	return &BaseDataModel{UserServiceModel: NewUserServiceModel()}
}
