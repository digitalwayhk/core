package manage

import (
	"github.com/digitalwayhk/core/internal/models"
	pt "github.com/digitalwayhk/core/pkg/persistence/types"

	st "github.com/digitalwayhk/core/pkg/server/types"
)

type Operation[T pt.IModel] struct {
	list     *models.ModelList[T]
	instance interface{}
	Model    *T `json:"model"`
}

func NewOperation[T pt.IModel](instance interface{}) Operation[T] {
	return Operation[T]{
		instance: instance,
	}
}
func (own *Operation[T]) New(instance interface{}) st.IRouter {
	if own.instance == nil {
		if prh, ok := instance.(st.IPackRouterHook); ok {
			own.instance = prh.GetInstance()
		} else {
			own.instance = instance
		}
	}
	if own.list == nil {
		own.list = models.NewManageModelList[T]()
	}
	return own
}
func (own *Operation[T]) Parse(req st.IRequest) error {
	if ms, ok := own.instance.(IManageService); ok {
		err := ms.ParseBefore(own, req)
		if err != nil {
			return err
		}
	}
	model := own.list.NewItem()
	err := req.Bind(model)
	if err != nil {
		return err
	}
	own.Model = model
	if ms, ok := own.instance.(IManageService); ok {
		err := ms.ParseAfter(own, req)
		if err != nil {
			return err
		}
	}
	if gml, ok := own.instance.(IGetModelList[T]); ok {
		own.list = gml.GetList()
	}
	return nil
}
func (own *Operation[T]) Validation(req st.IRequest) error {
	return nil
}
func (own *Operation[T]) ValidationBefore(sender interface{}, req st.IRequest) (error, bool) {
	if ms, ok := own.instance.(IManageService); ok {
		err, stop := ms.ValidationBefore(sender, req)
		if err != nil {
			return err, true
		}
		return err, stop
	}
	return nil, false
}
func (own *Operation[T]) ValidationAfter(sender interface{}, req st.IRequest) error {
	if ms, ok := own.instance.(IManageService); ok {
		err := ms.ValidationAfter(sender, req)
		if err != nil {
			return err
		}
	}
	return nil
}
func (own *Operation[T]) Do(req st.IRequest) (interface{}, error) {
	return own.Model, nil
}
func (own *Operation[T]) RouterInfo() *st.RouterInfo {
	return RouterInfo(own)
}
func (own *Operation[T]) GetInstance() interface{} {
	return own.instance
}
