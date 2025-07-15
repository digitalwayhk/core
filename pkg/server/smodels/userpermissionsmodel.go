package smodels

import "github.com/digitalwayhk/core/pkg/persistence/entity"

type UserPeermissionsModel struct {
	*entity.Model
	UserID             string `json:"userid"`             //用户ID
	PermissionsModelID uint   `json:"permissionsmodelid"` //权限ID
}

func NewUserPeermissionsModel() *UserPeermissionsModel {
	return &UserPeermissionsModel{
		Model: entity.NewModel(),
	}
}
func (own *UserPeermissionsModel) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}
