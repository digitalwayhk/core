package smodels

import "github.com/digitalwayhk/core/pkg/persistence/entity"

type PermissionsModel struct {
	*entity.Model
	MenuModelID uint   `json:"menumodelid"` //菜单ID
	Name        string `json:"name"`        //权限名称
	Title       string `json:"title"`       //权限标题
	Description string `json:"description"` //权限描述
	Sort        int    `json:"sort"`        //排序
	Icon        string `json:"icon"`        //图标
	Url         string `json:"url"`         //权限链接
}

func NewPermissionsModel() *PermissionsModel {
	return &PermissionsModel{
		Model: entity.NewModel(),
	}
}
func (own *PermissionsModel) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}
