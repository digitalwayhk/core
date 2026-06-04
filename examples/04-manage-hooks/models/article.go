package models

import "github.com/digitalwayhk/core/pkg/persistence/entity"

// Article 演示内容管理模型。
type Article struct {
	*entity.Model
	Title   string `json:"title" desc:"标题"`
	Content string `json:"content" desc:"正文"`
	Status  string `json:"status" desc:"状态"`
}

// NewModel 供 ModelList.NewItem 初始化基础字段。
func (own *Article) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}
