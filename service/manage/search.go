package manage

import (
	"encoding/json"
	"reflect"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	pt "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/digitalwayhk/core/service/manage/view"
)

type Search[T pt.IModel] struct {
	instance   interface{}
	list       *entity.ModelList[T]
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
	if gml, ok := own.instance.(IGetModelList); ok {
		list := gml.GetList()
		own.list = list.(*entity.ModelList[T])
	}
	return own
}

func (own *Search[T]) Parse(req types.IRequest) error {
	if ms, ok := own.instance.(IRequestSet); ok {
		ms.SetReq(req)
	}
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
	return nil
}
func (own *Search[T]) Validation(req types.IRequest) error {
	if ms, ok := own.instance.(IRequestSet); ok {
		ms.SetReq(req)
	}
	if own.SearchItem.Page == 0 {
		own.SearchItem.Page = 1
	}
	if own.SearchItem.Size == 0 {
		own.SearchItem.Size = 10
	}
	return nil
}
func (own *Search[T]) Do(req types.IRequest) (interface{}, error) {
	if ms, ok := own.instance.(IRequestSet); ok {
		ms.SetReq(req)
	}
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
	if bm, ok := item.Model.(pt.IIsPreload); ok {
		item.IsPreload = bm.IsPreload()
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

	ps := own.SearchItem.ToSearchItem()
	ps.Model = own.list.NewItem()
	sf := utils.GetPropertyType(ps.Model, own.SearchItem.Foreign.OneObjectField)
	if sf != nil {
		vf := sf.Type.Elem()
		ps.Model = utils.NewInterfaceByType(vf)
		if imn, ok := ps.Model.(pt.IModelNewHook); ok {
			imn.NewModel()
		}
		sliceType := reflect.SliceOf(sf.Type)
		// 修复：创建切片并获取其指针
		resultSlice := reflect.MakeSlice(sliceType, 0, ps.Size)
		resultPtr := reflect.New(sliceType)
		resultPtr.Elem().Set(resultSlice)
		result := resultPtr.Interface()

		var action pt.IDataAction = own.list.GetDBAdapter(ps)
		err := action.Load(ps, result)
		if err != nil {
			return nil, err
		}
		data := &view.ForeigData{
			Rows:  resultPtr.Elem().Interface(), // 取实际的切片值
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
	if own.SearchItem.ChildModel.References == "" {
		childitem.AddWhereN(own.SearchItem.ChildModel.ForeignKey, im.GetID())
	} else {
		childitem.AddWhereN(own.SearchItem.ChildModel.ForeignKey, utils.GetPropertyValue(parent, own.SearchItem.ChildModel.References))
	}
	sf := utils.GetPropertyType(parent, own.SearchItem.ChildModel.Name)
	if sf != nil {
		vf := sf.Type.Elem()
		childitem.Model = utils.NewInterfaceByType(vf)
		if imn, ok := childitem.Model.(pt.IModelNewHook); ok {
			imn.NewModel()
		}
		sliceType := reflect.SliceOf(vf)
		// 修复：创建切片并获取其指针
		resultSlice := reflect.MakeSlice(sliceType, 0, childitem.Size)
		resultPtr := reflect.New(sliceType)
		resultPtr.Elem().Set(resultSlice)
		result := resultPtr.Interface()

		childitem.IsPreload = true
		var action pt.IDataAction = own.list.GetDBAdapter(childitem)
		err := action.Load(childitem, result)
		if err != nil {
			return nil, err
		}
		if ms, ok := own.instance.(IManageSearch); ok {
			data := ms.OnSearchData(resultPtr.Elem().Interface(), childitem.Total)
			return ms.ChildSearchAfter(own, data, req)
		}
		return nil, err
	}
	return nil, nil
}
