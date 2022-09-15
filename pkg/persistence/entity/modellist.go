package entity

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"

	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/logx"
)

type ModelList[T types.IModel] struct {
	entityType  reflect.Type
	entityValue reflect.Value
	hideEntity  *T
	addList     []*T
	updateList  []*T
	deleteList  []*T
	searchList  []*T
	searchItem  *types.SearchItem
	ada         types.IDataAction
}

func NewModelList[T types.IModel](action types.IDataAction) *ModelList[T] {
	model := new(T)
	newmodel(model)
	t, v := utils.GetTypeAndValue(*model)
	if action == nil {
		if name := getdbname(model); name != "" {
			sl := oltp.NewSqlite()
			sl.Name = name
			action = sl
		}
	}
	list := newlist(t, v, model, action)
	list.searchItem = &types.SearchItem{
		Page:  1,
		Size:  10,
		Model: list.NewItem(),
	}
	return list
}
func newmodel(model interface{}) {
	defer func() {
		if err := recover(); err != nil {
			msg := errors.New(utils.GetTypeName(model) + "未实现IModelNewHook接口")
			logx.Error(msg)
		}
	}()
	if mo, ok := model.(types.IModelNewHook); ok {
		mo.NewModel()
	}
}
func getdbname(model interface{}) string {
	if dbn, ok := model.(types.IDBName); ok {
		return dbn.GetLocalDBName()
	}
	return ""
}
func newlist[T types.IModel](t reflect.Type, v reflect.Value, model *T, action types.IDataAction) *ModelList[T] {
	return &ModelList[T]{
		entityType:  t,
		entityValue: v,
		hideEntity:  model,
		addList:     make([]*T, 0, 1000),
		updateList:  make([]*T, 0, 1000),
		deleteList:  make([]*T, 0, 1000),
		searchList:  make([]*T, 0, 1000),
		searchItem: &types.SearchItem{
			Page: 1,
			Size: 10,
		},
		ada: action,
	}
}
func (own *ModelList[T]) GetSearchItem() *types.SearchItem {
	if own.searchItem == nil {
		own.searchItem = &types.SearchItem{
			Page: 1,
			Size: 10,
		}
	}
	return &types.SearchItem{
		Total:     0,
		Page:      1,
		Size:      own.searchItem.Size,
		Model:     own.searchItem.Model,
		WhereList: make([]*types.WhereItem, 0),
		SortList:  make([]*types.SortItem, 0),
	}
}
func (own *ModelList[T]) NewItem() *T {
	item := new(T)
	modelNewHook(item)
	return item
}
func modelNewHook(item interface{}) {
	if imn, ok := item.(types.IModelNewHook); ok {
		imn.NewModel()
	}
}
func modelAddValid[T types.IModel](list *ModelList[T], item interface{}) error {
	if im, ok := item.(types.IModel); ok {
		id := im.GetID()
		hash := getHash(item)
		if id == 0 {
			if hash == "" || utils.HashCodes("0") == hash {
				return nil
			}
		}
		list.searchItem.Model = item
		old, err := list.searchIdAndHash(id, hash)
		list.searchItem.Model = nil
		if err != nil {
			return err
		}
		if old != nil {
			return errors.New("id or hash already exists id:" + strconv.Itoa(int(id)) + " hash:" + hash)
		}
		setHash(item, hash)
		if imv, ok := item.(types.IModelValidHook); ok {
			return imv.AddValid()
		}
	}
	return nil
}
func getId(item interface{}) uint {
	if im, ok := item.(types.IModel); ok {
		return im.GetID()
	}
	return 0
}
func getHash(item interface{}) string {
	if im, ok := item.(types.IRowCode); ok {
		return im.GetHash()
	}
	return ""
}
func setHash(item interface{}, hash string) {
	if im, ok := item.(types.IRowCode); ok {
		im.SetHashcode(hash)
	}
}
func modelUpdateValid[T types.IModel](list *ModelList[T], item interface{}) error {
	if im, ok := item.(types.IModel); ok {
		list.searchItem.Model = item
		old, err := list.SearchId(im.GetID())
		list.searchItem.Model = nil
		if err != nil {
			return err
		}
		if old == nil {
			return errors.New("update item not found")
		}
		if rc, ok := item.(types.IRowCode); ok {
			hash := rc.GetHash()
			if hash != "" && getHash(old) != hash {
				values, err := list.SearchWhere("Hashcode", hash)
				if err != nil {
					return err
				}
				if len(values) > 0 {
					return errors.New("hash already exists hash:" + hash)
				}
			}
			rc.SetHashcode(hash)
		}
		if imv, ok := item.(types.IModelValidHook); ok {
			return imv.UpdateValid(old)
		}
	}
	return nil
}
func modelRemoveValid(item interface{}) error {
	if imv, ok := item.(types.IModelValidHook); ok {
		return imv.RemoveValid()
	}
	return nil
}
func (own *ModelList[T]) Add(items ...*T) error {
	for _, item := range items {
		if err := modelAddValid(own, item); err != nil {
			return err
		}
		own.addList = append(own.addList, item)
	}
	return nil
}
func (own *ModelList[T]) Update(items ...*T) error {
	for _, item := range items {
		if err := modelUpdateValid(own, item); err != nil {
			return err
		}
		own.updateList = append(own.updateList, item)
	}
	return nil
}
func (own *ModelList[T]) Remove(items ...*T) error {
	for _, item := range items {
		if err := modelRemoveValid(item); err != nil {
			return err
		}
		own.deleteList = append(own.deleteList, item)
	}
	return nil
}

func (own *ModelList[T]) RemoveAll(fn func(o *T) bool) {
	for i := 0; i < len(own.searchList); i++ {
		if fn(own.searchList[i]) {
			own.searchList = append(own.searchList[:i], own.searchList[i+1:]...)
			i--
		}
	}
}
func (own *ModelList[T]) ForEach(issearch bool, fn func(o *T)) {
	if issearch {
		for _, item := range own.searchList {
			fn(item)
		}
	} else {
		for _, item := range own.addList {
			fn(item)
		}
		for _, item := range own.updateList {
			fn(item)
		}
	}
}
func (own *ModelList[T]) FindAll(fn func(o *T) bool) []*T {
	items := make([]*T, 0)
	for _, item := range own.searchList {
		if fn(item) {
			items = append(items, item)
		}
	}
	for _, item := range own.addList {
		if fn(item) {
			items = append(items, item)
		}
	}
	for _, item := range own.updateList {
		if fn(item) {
			items = append(items, item)
		}
	}
	return items
}
func (own *ModelList[T]) FindOne(fn func(o *T) bool) (int, *T) {
	for index, item := range own.searchList {
		if fn(item) {
			return index, item
		}
	}
	for index, item := range own.addList {
		if fn(item) {
			return index, item
		}
	}
	for index, item := range own.updateList {
		if fn(item) {
			return index, item
		}
	}
	return -1, nil
}
func (own *ModelList[T]) Get(index int) *T {
	if len(own.searchList)-1 < index {
		return nil
	}
	return own.searchList[index]
}
func (own *ModelList[T]) searchIdAndHash(id uint, hash string) (*T, error) {
	item := own.GetSearchItem()
	item.AddWhere(&types.WhereItem{Column: "Id", Value: id})
	item.AddWhere(&types.WhereItem{Column: "Hashcode", Value: hash, Relation: "Or"})
	err := own.load(item)
	if err != nil {
		return nil, err
	}
	if len(own.searchList) > 0 {
		return own.searchList[0], nil
	}
	return nil, nil
}
func (own *ModelList[T]) SearchId(id uint, fn ...func(item *types.SearchItem)) (*T, error) {
	list, err := own.SearchWhere("Id", id, fn...)
	if err != nil {
		return nil, err
	}
	if len(list) > 0 {
		return list[0], nil
	}
	return nil, nil
}
func (own *ModelList[T]) SearchCode(code string, fn ...func(item *types.SearchItem)) (*T, error) {
	list, err := own.SearchWhere("code", code, fn...)
	if err != nil {
		return nil, err
	}
	if len(list) > 0 {
		return list[0], nil
	}
	return nil, nil
}

//Contains 是否包含该数据，如果包含，返回true，并返回ID
func (own *ModelList[T]) Contains(item interface{}) (bool, uint) {
	hash := getHash(item)
	own.SearchWhere("Hashcode", hash)
	if len(own.searchList) > 0 {
		return true, getId(own.searchList[0])
	}
	return false, 0
}

func (own *ModelList[T]) SearchName(name string, fn ...func(item *types.SearchItem)) ([]*T, error) {
	list, err := own.SearchWhere("Name", name, fn...)
	if err != nil {
		return nil, err
	}
	return list, nil
}
func (own *ModelList[T]) SearchWhere(name string, value interface{}, fn ...func(item *types.SearchItem)) ([]*T, error) {
	item := own.GetSearchItem()
	item.AddWhereN(name, value)
	for _, f := range fn {
		f(item)
	}
	var err error
	if item.IsPreload {
		err = own.PreLoadList(item)
	} else {
		err = own.LoadList(item)
	}
	if err != nil {
		return nil, err
	}
	return own.searchList, nil

}
func (own *ModelList[T]) SearchTraceId(traceid string, fn ...func(item *types.SearchItem)) (*T, error) {
	items, err := own.SearchWhere("TraceId", traceid, fn...)
	if err != nil {
		return nil, err
	}
	if len(items) > 0 {
		return items[0], nil
	}
	return nil, nil
}
func (own *ModelList[T]) SearchAll(page, size int, fn ...func(item *types.SearchItem)) ([]*T, int64, error) {
	item := own.GetSearchItem()
	if page > 0 {
		item.Page = page
	}
	if size > 0 {
		item.Size = size
	}
	for _, f := range fn {
		f(item)
	}
	err := own.LoadList(item)
	return own.searchList, item.Total, err
}

//统计查询，默认sum(field),当需要使用max,min,avg等统计时，需要在fn中修改item.Statistical=max
func (own *ModelList[T]) SerachSum(field string, fn ...func(item *types.SearchItem)) (decimal.Decimal, error) {
	item := own.GetSearchItem()
	item.IsStatistical = true
	item.StatField = field
	if len(fn) > 0 {
		for _, f := range fn {
			f(item)
		}
	}
	data := decimal.Zero
	err := own.ada.Load(item, &data)
	return data, err
}
func (own *ModelList[T]) LoadList(item *types.SearchItem) error {
	err := own.load(item)
	return err
}
func (own *ModelList[T]) PreLoadList(item *types.SearchItem) error {
	item.IsPreload = true
	err := own.load(item)
	return err
}
func (own *ModelList[T]) ReLoadList() error {
	return own.load(own.searchItem)
}
func (own *ModelList[T]) ToArray() []*T {
	return own.searchList
}
func (own *ModelList[T]) Count() int {
	count := len(own.searchList)
	return count
}
func (own *ModelList[T]) AddCount() int {
	return len(own.addList)
}
func (own *ModelList[T]) Clear() {
	own.searchList = make([]*T, 0, 1000)
	own.addList = make([]*T, 0, 1000)
	own.updateList = make([]*T, 0, 1000)
	own.deleteList = make([]*T, 0, 1000)
}
func (own *ModelList[T]) load(item *types.SearchItem) error {
	defer func() {
		if err := recover(); err != nil {
			msg := errors.New(utils.GetTypeName(own.hideEntity) + " 基础类型未初始化,缺失 NewModel 方法  error:" + fmt.Sprint(err))
			logx.Error(msg)
		}
	}()
	own.searchList = make([]*T, 0)
	if item.Model == nil {
		item.Model = own.hideEntity
	}
	if ish, ok := item.Model.(types.IModelSearchHook); ok {
		ws, err := ish.SearchWhere(item.WhereList)
		if err != nil {
			return err
		}
		for _, w := range ws {
			utils.SetPropertyValue(item.Model, w.Column, w.Value)
		}
		item.WhereList = ws
	}
	ada := own.GetDBAdapter()
	err := ada.Load(item, &own.searchList)
	return err
}
func (own *ModelList[T]) Save() error {
	ada := own.GetDBAdapter()
	for _, item := range own.addList {
		err := ada.Insert(item)
		if err != nil {
			return err
		}
	}
	for _, item := range own.updateList {
		err := ada.Update(item)
		if err != nil {
			return err
		}
	}
	for _, item := range own.deleteList {
		err := ada.Delete(item)
		if err != nil {
			return err
		}
	}
	own.addList = make([]*T, 0, 1000)
	own.updateList = make([]*T, 0, 1000)
	own.deleteList = make([]*T, 0, 1000)
	return nil
}
func (own *ModelList[T]) GetDBAdapter() types.IDataAction {
	return own.ada
}
func (own *ModelList[T]) SetDBAdapter(ada types.IDataAction) {
	own.ada = ada
}
