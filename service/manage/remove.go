package manage

import (
	"errors"

	pt "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type Remove[T pt.IModel] struct {
	Operation[T]
	Ids []uint `json:"ids"`
}

func NewRemove[T pt.IModel](instance interface{}) *Remove[T] {
	return &Remove[T]{
		Operation: NewOperation[T](instance),
	}
}
func (own *Remove[T]) New(instance interface{}) types.IRouter {
	if own.instance == nil {
		own.Operation.New(instance)
	}
	return own
}
func (own *Remove[T]) Validation(req types.IRequest) error {
	err, stop := own.Operation.ValidationBefore(own, req)
	if err != nil {
		return err
	}
	if stop {
		return err
	}
	id := (*own.Model).GetID()
	old, err := own.list.SearchId(id)
	if err != nil {
		return err
	}
	if old == nil {
		return errors.New("remove item not found")
	}
	err = own.Operation.ValidationAfter(own, req)
	return err
}
func modelRemoveValid(item interface{}) error {
	// model := item.(models.Model)
	// if model.State > 0 {
	// 	return errors.New("model state >0 can not remove")
	// }
	return nil
}
func (own *Remove[T]) Do(req types.IRequest) (interface{}, error) {
	if remove, ok := own.instance.(IManageService); ok {
		data, err, stop := remove.DoBefore(own, req)
		if stop {
			return data, err
		}
		if err != nil {
			return nil, err
		}
	}
	err := own.list.Remove(own.Model)
	if err != nil {
		return nil, err
	}
	err = own.list.Save()
	if err != nil {
		return nil, err
	}
	if remove, ok := own.instance.(IManageService); ok {
		data, err := remove.DoAfter(own, req)
		return data, err
	}
	return own.Model, err
}
func (own *Remove[T]) RouterInfo() *types.RouterInfo {
	return RouterInfo(own)
}
