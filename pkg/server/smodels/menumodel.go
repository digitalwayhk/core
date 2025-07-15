package smodels

import "github.com/digitalwayhk/core/pkg/persistence/entity"

type MenuModel struct {
	*entity.Model
	Name             string              `json:"name"`                                                            //菜单名称
	Title            string              `json:"title"`                                                           //菜单标题
	Description      string              `json:"description"`                                                     //菜单描述
	Sort             int                 `json:"sort"`                                                            //排序
	Icon             string              `json:"icon"`                                                            //图标
	Url              string              `json:"url"`                                                             //菜单链接
	Permissions      []*PermissionsModel `json:"permissions" gorm:"foreignkey:MenuModelID"`                       //权限列表
	DirectoryModelID uint                `json:"DirectoryModelID"`                                                //父菜单ID
	DirectoryModel   *DirectoryModel     `json:"DirectoryModel" gorm:"foreignkey:ID;references:DirectoryModelID"` //目录模型
}

func NewMenuModel() *MenuModel {
	return &MenuModel{
		Model: entity.NewModel(),
	}
}
func (own *MenuModel) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}
func (own *MenuModel) IsPreload() bool {
	return true
}
func (own *MenuModel) SetCode(code string) {

}
