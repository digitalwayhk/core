package manage

import (
	"errors"

	pt "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

type Edit[T pt.IModel] struct {
	Operation[T]
	OldItem    *T
	EditRecord interface{}
}

func NewEdit[T pt.IModel](instance interface{}) *Edit[T] {
	return &Edit[T]{
		Operation: NewOperation[T](instance),
	}
}
func (own *Edit[T]) New(instance interface{}) types.IRouter {
	if own.instance == nil {
		own.Operation.New(instance)
	}
	return own
}
func (own *Edit[T]) Validation(req types.IRequest) error {
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
	id := (*own.Model).GetID()
	if id == 0 {
		return errors.New("id 不能为空")
	}
	old, err := own.list.SearchId(id)
	if err != nil {
		return err
	}
	if old == nil {
		return errors.New("edit item not found")
	}
	own.OldItem = old
	err = own.Operation.ValidationAfter(own, req)
	return err
}
func modelEditValid(item interface{}) error {
	// model := item.(models.Model) //TODO: check interface 不能转换为基Struct
	// if model.State > 0 {
	// 	return errors.New("model state >0 can not edit")
	// }
	return nil
}
func (own *Edit[T]) Do(req types.IRequest) (interface{}, error) {
	if ms, ok := own.instance.(IRequestSet); ok {
		ms.SetReq(req)
	}
	if edit, ok := own.instance.(IManageService); ok {
		data, err, stop := edit.DoBefore(own, req)
		if stop {
			return data, err
		}
		if err != nil {
			return nil, err
		}
	}
	uid, uname := req.GetUser()
	if uid > 0 {
		utils.SetPropertyValue(own.Model, "UpdatedUser", uid)
	}
	if uname != "" {
		utils.SetPropertyValue(own.Model, "UpdatedUserName", uname)
	}
	utils.ForEach(own.Model, func(key string, value interface{}) {
		if key != "TraceID" && key != "Hashcode" && key != "DeletedAt" {
			utils.SetPropertyValue(own.OldItem, key, value)
		}
		// if utils.IsArray(value) {
		// 	utils.ArrayEach(value, func(v interface{}) {
		// 		ms := utils.GetPropertyValue(v, "ModelState")
		// 		if ms == 1 {
		// 			utils.SetPropertyValue(v, "ID", 0)
		// 		}
		// 		// if ms == 3 {

		// 		// }
		// 	})
		// }
	})

	err := own.list.Update(own.OldItem)
	if err != nil {
		return nil, err
	}
	err = own.list.Save()
	if err != nil {
		return nil, err
	}
	if edit, ok := own.instance.(IManageService); ok {
		data, err := edit.DoAfter(own, req)
		return data, err
	}
	return own.Model, err
}
func (own *Edit[T]) RouterInfo() *types.RouterInfo {
	return RouterInfo(own)
}
