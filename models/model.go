package models

import (
	"github.com/digitalwayhk/core/pkg/persistence/adapter"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/persistence/types"
)

type Model struct {
	*entity.Model
	State int
}
type ModelList[T types.IModel] struct {
	*entity.ModelList[T]
}

func NewModel() *Model {
	return &Model{
		Model: entity.NewModel(),
	}
}
func (own *Model) NewModel() {
	if own == nil {
		own = NewModel()
	}
}

func NewMongoModelList[T types.IModel]() *ModelList[T] {
	return &ModelList[T]{
		entity.NewModelList[T](adapter.NewNosqlAdapter()),
	}
}

func NewManageModelList[T types.IModel]() *ModelList[T] {
	ada := adapter.NewDefaultAdapter()
	ada.SetSaveType(types.OnlyLocal)
	return &ModelList[T]{
		entity.NewModelList[T](ada),
	}
}

func NewModelList[T types.IModel](action types.IDataAction) *ModelList[T] {
	return &ModelList[T]{
		entity.NewModelList[T](action),
	}
}
