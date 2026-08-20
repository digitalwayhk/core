package smodels

import (
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/utils"
)

type MenuModel struct {
	*entity.Model
	Name             string              `json:"name"`                                                            //菜单名称
	Title            string              `json:"title"`                                                           //菜单标题，默认中文展示，getmenu 已按当前语言填充
	TitleEN          string              `json:"titleen"`                                                         //菜单英文标题，为空时回退 Title
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
func (own *MenuModel) GetHash() string {
	return utils.HashCodes(own.Url)
}
func (own *MenuModel) IsPreload() bool {
	return true
}
func (own *MenuModel) SetCode(code string) {

}
