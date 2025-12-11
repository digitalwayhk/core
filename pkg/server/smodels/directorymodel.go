package smodels

import (
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/utils"
)

type DirectoryModel struct {
	*entity.Model
	Name        string `json:"name"`        //目录名称
	Title       string `json:"title"`       //目录标题
	Description string `json:"description"` //目录描述
	ParentID    int64  `json:"parentid"`    //父目录ID
	Sort        int    `json:"sort"`        //排序
	Icon        string `json:"icon"`        //图标
	Url         string `json:"url"`         //目录链接
	//Children    []*DirectoryModel `json:"children" gorm:"foreignkey:ParnetID"`          //子目录
	MenuItems []*MenuModel `json:"menuitems" gorm:"foreignkey:DirectoryModelID"` //菜单项
}

func NewDirectoryModel() *DirectoryModel {
	return &DirectoryModel{
		Model: entity.NewModel(),
	}
}

func (own *DirectoryModel) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}

func (own *DirectoryModel) GetHash() string {
	return utils.HashCodes(own.Name)
}
