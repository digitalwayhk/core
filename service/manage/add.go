package manage

import (
	pt "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

type Add[T pt.IModel] struct {
	Operation[T]
}

func NewAdd[T pt.IModel](instance interface{}) *Add[T] {
	return &Add[T]{
		Operation: NewOperation[T](instance),
	}
}
func (own *Add[T]) New(instance interface{}) types.IRouter {
	if own.instance == nil {
		own.Operation.New(instance)
	}
	return own
}
func (own *Add[T]) Validation(req types.IRequest) error {
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
func (own *Add[T]) Do(req types.IRequest) (interface{}, error) {
	if ms, ok := own.instance.(IRequestSet); ok {
		ms.SetReq(req)
	}
	if add, ok := own.instance.(IManageService); ok {
		data, err, stop := add.DoBefore(own, req)
		if stop {
			return data, err
		}
		if err != nil {
			return nil, err
		}
	}
	utils.SetPropertyValue(own.Model, "ID", req.NewID())
	uid, uname := req.GetUser()
	if uid > 0 {
		utils.SetPropertyValue(own.Model, "CreatedUser", uid)
	}
	if uname != "" {
		utils.SetPropertyValue(own.Model, "CreatedUserName", uname)
	}
	err := own.list.Add(own.Model)
	if err != nil {
		return nil, err
	}
	err = own.list.Save()
	if err != nil {
		return nil, err
	}
	if add, ok := own.instance.(IManageService); ok {
		data, err := add.DoAfter(own, req)
		return data, err
	}
	return own.Model, err
}
func (own *Add[T]) RouterInfo() *types.RouterInfo {
	return RouterInfo(own)
}
