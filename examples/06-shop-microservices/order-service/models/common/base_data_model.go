package common

type BaseDataModel struct {
	*OrderServiceModel
}

func NewBaseDataModel() *BaseDataModel {
	return &BaseDataModel{OrderServiceModel: NewOrderServiceModel()}
}
