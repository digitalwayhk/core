package manage

import (
	"encoding/json"
	"reflect"

	"github.com/digitalwayhk/core/internal/manage/view"
	"github.com/digitalwayhk/core/internal/models"
	pt "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
)

type Search[T pt.IModel] struct {
	instance   interface{}
	list       *models.ModelList[T]
	SearchItem *view.SearchItem
	View       *view.ViewModel
}

func NewSearch[T pt.IModel](instance interface{}) *Search[T] {
	return &Search[T]{
		instance: instance,
	}
}
func (own *Search[T]) New(instance interface{}) types.IRouter {
	if own.instance == nil {
		if prh, ok := instance.(types.IPackRouterHook); ok {
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
func (own *Search[T]) GetModelList() *models.ModelList[T] {
	return own.list
}
func (own *Search[T]) Parse(req types.IRequest) error {
	own.SearchItem = &view.SearchItem{}
	err := req.Bind(own.SearchItem)
	if err != nil {
		return err
	}
	if own.SearchItem.Parent != nil {
		item := own.list.NewItem()
		values, err := json.Marshal(own.SearchItem.Parent)
		if err != nil {
			return err
		}
		err = json.Unmarshal(values, item)
		own.SearchItem.Parent = item
		return err
	}

	if gml, ok := own.instance.(IGetModelList[T]); ok {
		own.list = gml.GetList()
	}
	return nil
}
func (own *Search[T]) Validation(req types.IRequest) error {
	return nil
}
func (own *Search[T]) Do(req types.IRequest) (interface{}, error) {
	var view IManageView = own.instance.(IManageView)
	if view != nil {
		own.View = view.GetView()
	}
	if own.SearchItem.Field != nil && own.SearchItem.Foreign != nil {
		return own.foreignSearch(req)
	}
	if own.SearchItem.Parent != nil && own.SearchItem.ChildModel != nil {
		return own.childSearch(req)
	}
	own.SearchItem.View = own.View
	if ms, ok := own.instance.(IManageSearch); ok {
		data, err, stop := ms.SearchBefore(own, req)
		if stop {
			return data, err
		}
		if err != nil {
			return nil, err
		}
	}
	list := own.list
	item := own.SearchItem.ToSearchItem()
	item.Model = list.NewItem()
	if bm, ok := item.Model.(pt.IBaseModel); ok {
		item.IsPreload = bm.IsBaseModel()
	}
	err := list.LoadList(item)
	if err != nil {
		return nil, err
	}
	if ms, ok := own.instance.(IManageSearch); ok {
		data := ms.OnSearchData(list.ToArray(), item.Total)
		return ms.SearchAfter(own, data, req)
	}
	return nil, err
}
func (own *Search[T]) RouterInfo() *types.RouterInfo {
	return RouterInfo(own)
}
func (own *Search[T]) GetInstance() interface{} {
	return own.instance
}

func (own *Search[T]) foreignSearch(req types.IRequest) (interface{}, error) {
	own.View = own.SearchItem.Field.Foreign.FModel
	own.SearchItem.View = own.View
	if ms, ok := own.instance.(IManageSearch); ok {
		data, err, stop := ms.ForeignSearchBefore(own, req)
		if stop {
			return data, err
		}
		if err != nil {
			return nil, err
		}
	}
	var action pt.IDataAction = own.list.GetDBAdapter()
	ps := own.SearchItem.ToSearchItem()
	ps.Model = own.list.NewItem()
	sf := utils.GetPropertyType(ps.Model, own.SearchItem.Foreign.OneObjectField)
	if sf != nil {
		vf := sf.Type.Elem()
		if vf.Kind() == reflect.Ptr {
			vf = vf.Elem()
		}
		ps.Model = reflect.New(vf).Interface()
		if imn, ok := ps.Model.(pt.IModelNewHook); ok {
			imn.NewModel()
		}
		sliceType := reflect.SliceOf(sf.Type)
		result := reflect.MakeSlice(sliceType, 0, ps.Size).Interface()
		err := action.Load(ps, &result)
		if err != nil {
			return nil, err
		}
		data := &view.ForeigData{
			Rows:  result,
			Total: ps.Total,
			Model: own.SearchItem.Foreign.FModel,
		}
		if ms, ok := own.instance.(IManageSearch); ok {
			return ms.ForeignSearchAfter(own, data, req)
		}
		return data, err
	}
	return nil, nil
}
func (own *Search[T]) childSearch(req types.IRequest) (interface{}, error) {
	var view IManageView = own.instance.(IManageView)
	if view != nil {
		vm := view.GetView()
		for _, v := range vm.ChildModels {
			if v.Name == own.SearchItem.ChildModel.Name {
				own.View = &v.ViewModel

			}
		}
	}
	own.SearchItem.View = own.View
	if ms, ok := own.instance.(IManageSearch); ok {
		data, err, stop := ms.ChildSearchBefore(own, req)
		if stop {
			return data, err
		}
		if err != nil {
			return nil, err
		}
	}
	var action pt.IDataAction = own.list.GetDBAdapter()

	childitem := own.SearchItem.ToSearchItem()
	parent := own.list.NewItem()
	values, err := json.Marshal(own.SearchItem.Parent)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(values, parent)
	if err != nil {
		return nil, err
	}
	var im pt.IModel = *parent
	childitem.AddWhereN(own.SearchItem.ChildModel.ForeignKey, im.GetID())
	sf := utils.GetPropertyType(parent, own.SearchItem.ChildModel.Name)
	if sf != nil {
		vf := sf.Type.Elem()
		if vf.Kind() == reflect.Ptr {
			vf = vf.Elem()
		}
		childitem.Model = reflect.New(vf).Interface()
		if imn, ok := childitem.Model.(pt.IModelNewHook); ok {
			imn.NewModel()
		}
		sliceType := reflect.SliceOf(vf)
		result := reflect.MakeSlice(sliceType, 0, childitem.Size).Interface()
		childitem.IsPreload = true
		err := action.Load(childitem, &result)
		if ms, ok := own.instance.(IManageSearch); ok {
			data := ms.OnSearchData(result, childitem.Total)
			return ms.ForeignSearchAfter(own, data, req)
		}
		return nil, err
	}
	return nil, nil
}
