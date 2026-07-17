package common

import "github.com/digitalwayhk/core/pkg/persistence/entity"

const DatabaseName = "shop-user"

type UserServiceModel struct {
	*entity.Model
}

func NewUserServiceModel() *UserServiceModel {
	return &UserServiceModel{Model: entity.NewModel()}
}

func (m *UserServiceModel) GetLocalDBName() string  { return DatabaseName }
func (m *UserServiceModel) GetRemoteDBName() string { return DatabaseName }
