package models

import "github.com/digitalwayhk/core/pkg/persistence/entity"

// Book 演示普通业务实体。所有可持久化实体通常嵌入 *entity.Model。
type Book struct {
	*entity.Model
	Title    string `json:"title" desc:"书名"`
	Author   string `json:"author" desc:"作者"`
	Category string `json:"category" desc:"分类"`
}

// NewBook 适合业务代码手动创建对象时使用。
func NewBook() *Book {
	return &Book{Model: entity.NewModel()}
}

// NewModel 供 ModelList.NewItem 调用，确保基础字段存在。
func (own *Book) NewModel() {
	if own.Model == nil {
		own.Model = entity.NewModel()
	}
}
