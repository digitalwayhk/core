package manage

import (
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	pt "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/types"
)

//Submit 提交，用于确认数据设置完成，该状态数据不可修改
type Submit[T pt.IModel] struct {
	Operation[T]
}

func NewSubmit[T pt.IModel](instance interface{}) *Submit[T] {
	return &Submit[T]{
		Operation: NewOperation[T](instance),
	}
}
func (own *Submit[T]) New(instance interface{}) types.IRouter {
	if own.instance == nil {
		own.Operation.New(instance)
	}
	return own
}
func (own *Submit[T]) Validation(req types.IRequest) error {
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
func (own *Submit[T]) Do(req types.IRequest) (interface{}, error) {
	obm := getIModel(own.Model)
	model, err := own.list.SearchId(obm.GetID())
	if err != nil {
		return nil, err
	}
	if submit, ok := own.instance.(IManageService); ok {
		data, err, stop := submit.DoBefore(own, req)
		if stop {
			return data, err
		}
		if err != nil {
			return nil, err
		}
	}
	bm := getbaseModel(model)
	if bm != nil {
		if bm.State == 0 {
			bm.State = 1
			own.list.Update(model)
			own.list.Save()
		}
	}
	if submit, ok := own.instance.(IManageService); ok {
		data, err := submit.DoAfter(own, req)
		return data, err
	}
	return own.Model, nil
}
func getIModel(instance interface{}) pt.IModel {
	return instance.(pt.IModel)
}
func getbaseModel(instance interface{}) *entity.BaseModel {
	return instance.(*entity.BaseModel)
}
func getorderModel(instance interface{}) *entity.BaseOrderModel {
	return instance.(*entity.BaseOrderModel)
}
func (own *Submit[T]) RouterInfo() *types.RouterInfo {
	return RouterInfo(own)
}
