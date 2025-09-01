package entity

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"sync"

	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"gorm.io/gorm"

	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/logx"
)

var (
	globalSqliteInstances = make(map[string]*oltp.Sqlite)
	sqliteInstanceMutex   = sync.RWMutex{}
)

func GetGlobalSqliteInstance(name string) *oltp.Sqlite {
	sqliteInstanceMutex.RLock()
	if instance, exists := globalSqliteInstances[name]; exists {
		sqliteInstanceMutex.RUnlock()
		return instance
	}
	sqliteInstanceMutex.RUnlock()

	sqliteInstanceMutex.Lock()
	defer sqliteInstanceMutex.Unlock()

	// 双重检查
	if instance, exists := globalSqliteInstances[name]; exists {
		return instance
	}

	// 创建新实例
	//logx.Infof("🆕 创建全局Sqlite实例: %s", name)
	instance := oltp.NewSqlite()
	instance.Name = name
	globalSqliteInstances[name] = instance

	return instance
}

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
	onLoad      func(item *types.SearchItem, own interface{}) error
	onInsert    func(item *T) error
	onUpdate    func(item *T) error
	onDelete    func(item *T) error
}

func NewModelList[T types.IModel](action types.IDataAction) *ModelList[T] {
	model := new(T)
	newmodel(model)
	t, v := utils.GetTypeAndValue(*model)

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

const (
	searchCap    = 20 // 搜索结果通常较多
	operationCap = 5  // 增删改操作通常较少
)

func newlist[T types.IModel](t reflect.Type, v reflect.Value, model *T, action types.IDataAction) *ModelList[T] {
	return &ModelList[T]{
		entityType:  t,
		entityValue: v,
		hideEntity:  model,
		addList:     make([]*T, 0, operationCap),
		updateList:  make([]*T, 0, operationCap),
		deleteList:  make([]*T, 0, operationCap),
		searchList:  make([]*T, 0, searchCap),
		searchItem: &types.SearchItem{
			Page: 1,
			Size: 10,
		},
		ada: action,
	}
}
func (own *ModelList[T]) SetOnLoad(fn func(item *types.SearchItem, own interface{}) error) {
	own.onLoad = fn
}
func (own *ModelList[T]) SetOnInsert(fn func(item *T) error) {
	own.onInsert = fn
}
func (own *ModelList[T]) SetOnUpdate(fn func(item *T) error) {
	own.onUpdate = fn
}
func (own *ModelList[T]) SetOnDelete(fn func(item *T) error) {
	own.onDelete = fn
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
func (own *ModelList[T]) SearchHash(hash string, fn ...func(item *types.SearchItem)) (*T, error) {
	list, err := own.SearchWhere("Hashcode", hash, fn...)
	if err != nil {
		return nil, err
	}
	if len(list) > 0 {
		return list[0], nil
	}
	return nil, nil
}

// Contains 是否包含该数据，如果包含，返回true，并返回ID
func (own *ModelList[T]) Contains(item interface{}) (bool, uint) {
	searchItem := own.GetSearchItem()
	searchItem.Model = item
	id := getId(item)
	if id > 0 {
		searchItem.AddWhereN("Id", id)
	}
	err := own.LoadList(searchItem)
	if err != nil {
		return false, 0
	}
	resBool := len(own.searchList) > 0
	if resBool {
		id = getId(own.searchList[0])
	} else {
		id = 0
	}
	return resBool, id
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
	if item.Size <= 10 {
		item.Size = 500
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
	if item.Total > 0 && item.Total > 500 {
		logx.Error(fmt.Sprintf("%s类型的SearchWhere条件查询数据超过500条,可能会影响性能,建议使用SearchAll方法指定分页查询:%s", utils.GetTypeName(own.hideEntity), utils.PrintObj(item)))
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
func (own *ModelList[T]) SearchOne(fn ...func(item *types.SearchItem)) (*T, error) {
	items, _, err := own.SearchAll(1, 1, fn...)
	if len(items) == 0 {
		return nil, err
	}
	return items[0], err
}

// 统计查询，默认sum(field),当需要使用max,min,avg等统计时，需要在fn中修改item.Statistical=max
func (own *ModelList[T]) SearchSum(field string, fn ...func(item *types.SearchItem)) (decimal.Decimal, error) {
	item := own.GetSearchItem()
	item.IsStatistical = true
	item.StatField = field
	if len(fn) > 0 {
		for _, f := range fn {
			f(item)
		}
	}
	data := decimal.Zero
	if err := own.searchHook(item); err != nil {
		return data, err
	}
	ada := own.GetDBAdapter(item)
	err := ada.Load(item, &data)
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
func (own *ModelList[T]) AddArray() []*T {
	return own.addList
}
func (own *ModelList[T]) UpdateArray() []*T {
	return own.updateList
}
func (own *ModelList[T]) RemoveArray() []*T {
	return own.deleteList
}
func (own *ModelList[T]) Count() int {
	count := len(own.searchList)
	return count
}
func (own *ModelList[T]) AddCount() int {
	return len(own.addList)
}
func (own *ModelList[T]) Clear() {
	own.searchList = make([]*T, 0, searchCap)
	own.addList = make([]*T, 0, operationCap)
	own.updateList = make([]*T, 0, operationCap)
	own.deleteList = make([]*T, 0, operationCap)
}
func (own *ModelList[T]) OnLoad(ada types.IDataAction, item *types.SearchItem) error {
	err := ada.Load(item, &own.searchList)
	if err == nil && own.onLoad != nil {
		err = own.onLoad(item, own)
	}
	return err
}
func (own *ModelList[T]) load(item *types.SearchItem) error {
	defer func() {
		if err := recover(); err != nil {
			msg := errors.New(utils.GetTypeName(own.hideEntity) + " 基础类型未初始化,缺失 NewModel 方法 或 error:" + fmt.Sprint(err) + "/r/n 查询信息:" + utils.PrintObj(item))
			logx.Error(msg)
		}
	}()
	if err := own.searchHook(item); err != nil {
		return err
	}
	if len(item.SortList) == 0 {
		item.AddSort(&types.SortItem{Column: "ID", IsDesc: true})
	}
	ada := own.GetDBAdapter(item)
	return own.OnLoad(ada, item)
}
func (own *ModelList[T]) searchHook(item *types.SearchItem) error {
	// 根据预期大小智能分配容量
	expectedSize := item.Size
	if expectedSize <= 0 {
		expectedSize = 10
	}

	capacity := getOptimalCapacity(expectedSize)

	// 重用现有切片的底层数组（如果容量足够）
	if cap(own.searchList) >= capacity {
		own.searchList = own.searchList[:0] // 重置长度但保持容量
	} else {
		own.searchList = make([]*T, 0, capacity)
	}

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
	return nil
}
func (own *ModelList[T]) OnInsert(ada types.IDataAction, item *T) error {
	var err error
	if own.onInsert != nil {
		err = own.onInsert(item)
		if err != nil {
			return err
		}
	}
	err = ada.Insert(item)
	return err
}
func (own *ModelList[T]) OnUpdate(ada types.IDataAction, item *T) error {
	// utils.ForEach(item, func(name string, value interface{}) {
	// 	if utils.IsArray(value) {
	// 		utils.ArrayEach(value, func(v interface{}) {
	// 			ms := utils.GetPropertyValue(v, "ModelState")
	// 			if ms == 1 {
	// 				utils.SetPropertyValue(v, "ID", 0)
	// 			}
	// 			if ms == 3 {
	// 				ada.Delete(v)
	// 			}
	// 		})
	// 	}
	// })
	var err error
	if err == nil && own.onUpdate != nil {
		err = own.onUpdate(item)
		if err != nil {
			return err
		}
	}
	err = ada.Update(item)
	return err

}
func (own *ModelList[T]) OnDelete(ada types.IDataAction, item *T) error {
	return ada.Delete(item)
}
func (own *ModelList[T]) Save() error {
	searchItem := own.GetSearchItem()
	for _, item := range own.addList {
		searchItem.Model = item
		ada := own.GetDBAdapter(searchItem)
		err := own.OnInsert(ada, item)
		if err != nil {
			return err
		}
	}
	for _, item := range own.updateList {
		searchItem.Model = item
		ada := own.GetDBAdapter(searchItem)
		err := own.OnUpdate(ada, item)
		if err != nil {
			return err
		}
	}
	for _, item := range own.deleteList {
		searchItem.Model = item
		ada := own.GetDBAdapter(searchItem)
		err := own.OnDelete(ada, item)
		if err != nil {
			return err
		}
	}
	own.addList = make([]*T, 0, operationCap)
	own.updateList = make([]*T, 0, operationCap)
	own.deleteList = make([]*T, 0, operationCap)
	return nil
}
func (own *ModelList[T]) GetDBAdapter(item *types.SearchItem) types.IDataAction {
	if own.ada == nil && item != nil && item.Model != nil {
		if name := getdbname(item.Model); name != "" {
			own.ada = GetGlobalSqliteInstance(name)
		}
	}
	return own.ada
}
func (own *ModelList[T]) SetDBAdapter(ada types.IDataAction) {
	own.ada = ada
}

func (own *ModelList[T]) GetDB() (*gorm.DB, error) {
	item := own.GetSearchItem()
	if item.Model == nil {
		item.Model = own.hideEntity
	}
	_adadb, err := own.GetDBAdapter(item).GetModelDB(own.hideEntity)
	if err != nil {
		return nil, err
	}
	if db, ok := _adadb.(*gorm.DB); ok {
		return db, nil
	}
	if items, ok := _adadb.([]types.IDataBase); ok {
		if len(items) > 0 {
			db, _ := items[0].GetModelDB(own.hideEntity)
			if db1, ok := db.(*gorm.DB); ok {
				return db1, nil
			}
		}
	}
	return nil, nil
}

func getOptimalCapacity(expectedSize int) int {
	if expectedSize <= 0 {
		return 5
	}
	if expectedSize <= 10 {
		return 10
	}
	if expectedSize <= 50 {
		return 50
	}
	if expectedSize <= 100 {
		return 100
	}
	// 最大预分配500，而不是1000
	if expectedSize <= 500 {
		return expectedSize
	}
	return 500
}

func (own *ModelList[T]) GetMemoryStats() map[string]int {
	return map[string]int{
		"searchList_cap": cap(own.searchList),
		"searchList_len": len(own.searchList),
		"addList_cap":    cap(own.addList),
		"addList_len":    len(own.addList),
		"updateList_cap": cap(own.updateList),
		"updateList_len": len(own.updateList),
		"deleteList_cap": cap(own.deleteList),
		"deleteList_len": len(own.deleteList),
	}
}
