package manage

import (
	"errors"

	pt "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type Release[T pt.IModel] struct {
	Operation[T]
}

func NewRelease[T pt.IModel](instance interface{}) *Release[T] {
	return &Release[T]{
		Operation: NewOperation[T](instance),
	}
}
func (own *Release[T]) New(instance interface{}) types.IRouter {
	if own.instance == nil {
		own.Operation.New(instance)
	}
	return own
}
func (own *Release[T]) Validation(req types.IRequest) error {
	if ms, ok := own.instance.(IRequestSet); ok {
		ms.SetReq(req)
	}
	err, stop := own.Operation.ValidationBefore(own, req)
	if err != nil {
		return err
	}
	if stop {
		return err
	}
	err = own.Operation.ValidationAfter(own, req)
	return err
}
func (own *Release[T]) Do(req types.IRequest) (interface{}, error) {
	if ms, ok := own.instance.(IRequestSet); ok {
		ms.SetReq(req)
	}
	id := (*own.Model).GetID()
	if id == 0 {
		return nil, errors.New("id is 0")
	}
	if release, ok := own.instance.(IManageService); ok {
		data, err, stop := release.DoBefore(own, req)
		if stop {
			return data, err
		}
		if err != nil {
			return nil, err
		}
	}
	err := own.list.Update(own.Model)
	if err != nil {
		return nil, err
	}
	return own.Model, nil
}

func (own *Release[T]) RouterInfo() *types.RouterInfo {
	return RouterInfo(own)
}
