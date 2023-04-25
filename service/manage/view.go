package manage

import (
	"errors"
	"reflect"
	"sort"
	"strings"

	"github.com/digitalwayhk/core/models"
	pt "github.com/digitalwayhk/core/pkg/persistence/types"
	types "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/digitalwayhk/core/service/manage/view"

	"github.com/zeromicro/go-zero/core/logx"
)

type View[T pt.IModel] struct {
	instance interface{}
	Model    *view.ViewModel
}

func NewView[T pt.IModel](instance interface{}) *View[T] {
	return &View[T]{
		instance: instance,
	}
}
func (own *View[T]) New(instance interface{}) types.IRouter {
	if own.instance == nil {
		if prh, ok := instance.(types.IPackRouterHook); ok {
			own.instance = prh.GetInstance()
		} else {
			own.instance = instance
		}
	}
	return own
}
func (own *View[T]) Parse(req types.IRequest) error {
	if ms, ok := own.instance.(IRequestSet); ok {
		ms.SetReq(req)
	}
	if ms, ok := own.instance.(IManageService); ok {
		err := ms.ParseBefore(own, req)
		if err != nil {
			return err
		}
	}
	return nil
}
func (own *View[T]) Validation(req types.IRequest) error {
	if ms, ok := own.instance.(IRequestSet); ok {
		ms.SetReq(req)
	}
	if _, ok := own.instance.(IManageService); !ok {
		return errors.New("instance is not IManageService")
	}
	return nil
}

var viewModelMap = make(map[string]view.ViewModel)

func (own *View[T]) Do(req types.IRequest) (interface{}, error) {
	if ms, ok := own.instance.(IRequestSet); ok {
		ms.SetReq(req)
	}
	// defer func() {
	// 	if err := recover(); err != nil {
	// 		logx.Error(err)
	// 	}
	// }()
	mv, vm := own.getmv(req)
	list := models.NewManageModelList[T]()
	model := list.NewItem()
	vm.Fields = modelToFiled(model, mv)
	if mv != nil {
		vm.ChildModels = modelToChildModel(model, mv)
		mv.ViewModel(vm)
	}
	own.Model = vm
	return own.Model, nil
}
func (own *View[T]) getmv(req types.IRequest) (IManageView, *view.ViewModel) {
	vm := &view.ViewModel{}
	if own.instance != nil {
		if mv, ok := own.instance.(IManageView); ok {
			if ms, ok := own.instance.(IManageService); ok {
				ms.DoBefore(own, req)
				vm.Name = utils.GetTypeName(own.instance)
				vm.Title = vm.Name
				vm.Commands = make([]*view.CommandModel, 0)
				vm.Fields = make([]*view.FieldModel, 0)
				for index, router := range ms.Routers() {
					info := router.RouterInfo()
					vm.ServiceName = info.ServiceName
					cmd := routerToCommand(info)
					if cmd != nil {
						cmd.Index = index
						if mv != nil {
							mv.ViewCommandModel(cmd)
						}
						vm.Commands = append(vm.Commands, cmd)
					}
				}
				return mv, vm
			} else {
				msg := utils.GetTypeName(own.instance) + " instance is not IManageService"
				logx.Error(msg)
				return nil, nil
			}
		} else {
			msg := utils.GetTypeName(own.instance) + " instance is not IManageView"
			logx.Error(msg)
			return nil, nil
		}
	}
	return nil, vm
}
func (own *View[T]) RouterInfo() *types.RouterInfo {
	return RouterInfo(own)
}
func (own *View[T]) GetInstance() interface{} {
	return own.instance
}
func routerToCommand(info *types.RouterInfo) *view.CommandModel {
	count := strings.Index(info.StructName, "[")
	name := info.StructName
	if count > 0 {
		name = info.StructName[0:count]
	}
	if name == "View" || name == "Search" {
		return nil
	}
	cmd := &view.CommandModel{
		Command: strings.ToLower(name),
		Name:    name,
		Title:   name,
	}
	if cmd.Command != "add" {
		cmd.IsSelectRow = true
	}
	if cmd.Command != "add" && cmd.Command != "edit" {
		cmd.IsAlert = true
	}
	// if cmd.Name == "Release" {
	// 	cmd.IsSplit = true
	// 	cmd.SplitName = "submit"
	// }
	return cmd
}
func getfieldname(field *reflect.StructField) (string, string) {
	if field == nil {
		return "", ""
	}
	name := field.Name
	jname := field.Tag.Get("json")
	posttype := ""
	if jname != "" {
		if index := strings.Index(jname, ","); index > 0 {
			name = jname[0:index]
			posttype = jname[index+1:]
		} else {
			name = jname
		}
	}
	return name, posttype
}
func modelToFiled(model interface{}, mv IManageView) []*view.FieldModel {
	fields := make([]*view.FieldModel, 0)
	foreignItems := make(map[string]*view.ForeignModel)

	utils.DeepForItem(model, func(field, parent reflect.StructField, kind utils.TypeKind) {
		if field.Name == "Valid" || field.Name == "Hashcode" || field.Name == "CreatedUser" || field.Name == "UpdatedUser" {
			return
		}
		typeName := field.Type.Name()

		if kind == utils.Base || (kind == utils.Struct && (typeName == "Time" || typeName == "Decimal")) {
			vfm := getField(field, typeName)
			fields = append(fields, vfm)
		}
		if (kind == utils.Struct || kind == utils.Ptr) && (typeName != "Time" && typeName != "Decimal") {
			name, fm := getForeignField(field, model)
			foreignItems[name] = fm
		}
	})
	count := len(fields)
	for index, item := range fields {
		if item.Type == "float64" || item.Type == "float32" || item.Type == "float" {
			item.Type = "decimal"
		}
		if item.Type == "decimal" {
			item.Precision = 4
			item.Min = 0
		}
		if item.Type == "bool" {
			item.Index = count + index
		}
		if item.IsFieldOrTitle("CreatedAt") || item.IsFieldOrTitle("UpdatedAt") {
			item.Index = count + index + count
		}
		if item.IsFieldOrTitle("CreatedUserName") || item.IsFieldOrTitle("UpdatedUserName") {
			item.Index = count + index + count
		}
		if fn, ok := foreignItems[item.Field]; ok {
			item.Foreign = fn
		}
		if _, ok := model.(pt.IBaseModel); ok {
			if item.IsFieldOrTitle("state") {
				item.Disabled = true
			}
			if item.IsFieldOrTitle("CreatedUserName") || item.IsFieldOrTitle("UpdatedUserName") {
				item.IsEdit = false
			}
			if item.IsFieldOrTitle("code") && item.IsEdit {
				item.Required = true
			}
			if item.IsFieldOrTitle("CreatedAt") || item.IsFieldOrTitle("UpdatedAt") {
				item.IsEdit = false
			}
			if item.IsFieldOrTitle("TraceID") {
				item.Visible = false
			}
		}
		if mv != nil {
			mv.ViewFieldModel(model, item)
		}
	}
	return fields
}
func getField(field reflect.StructField, typeName string) *view.FieldModel {
	vfm := &view.FieldModel{}
	vfm.Field, vfm.PostType = getfieldname(&field)
	vfm.Title = field.Name
	vfm.IsSearch = true
	vfm.IsEdit = true
	vfm.Visible = true
	vfm.Sorter = true
	vfm.FieldType = field.Type
	if typeName == "Time" {
		vfm.DataTimeType.SetDate(true)
		vfm.Type = "datetime"
	} else {
		vfm.Type = strings.ToLower(typeName)
	}

	if field.Name == "ID" {
		vfm.IsKey = true
		vfm.IsSearch = false
		vfm.IsEdit = false
		vfm.Visible = false
		vfm.Sorter = false
	}
	if field.Name == "Icon" {
		vfm.IsSearch = false
		vfm.Sorter = false
	}
	if field.Name == "CreatedAt" || field.Name == "UpdatedAt" || field.Name == "CreatedUserName" || field.Name == "UpdatedUserName" {
		vfm.IsEdit = false
	}
	return vfm
}
func gormfield(key string, field reflect.StructField) string {
	value := field.Tag.Get("gorm")
	if value == "" {
		return ""
	}
	//value = strings.ToLower(value)
	args := strings.Split(value, ";")
	for _, arg := range args {
		index := strings.Index(arg, key)
		if index >= 0 {
			name := arg[index+len(key):]
			return name
		}
	}
	return ""
}

var FOREIGNKEY = "foreignkey:"
var REFERENCES = "references:"

func getForeignField(field reflect.StructField, model interface{}) (string, *view.ForeignModel) {
	name := gormfield(REFERENCES, field)
	foreignkey := gormfield(FOREIGNKEY, field)
	if foreignkey == "" {
		foreignkey = "id"
	} else {
		foreignkey = strings.ToLower(foreignkey)
	}
	if name == "" {
		name = field.Name + "ID"
	}
	oof, _ := getfieldname(&field)
	fm := &view.ForeignModel{
		IsFkey:             true,
		OneObjectTypeName:  field.Type.Name(),
		OneObjectField:     oof,
		OneObjectName:      field.Type.Name(),
		OneObjectFieldKey:  foreignkey,
		OneDisplayName:     "name",
		ManyObjectTypeName: utils.GetTypeName(model),
		ManyObjectName:     utils.GetTypeName(model),
		ManyObjectField:    field.Name,
		ManyObjectFieldKey: name,
	}
	fm.FModel = &view.ViewModel{
		Name:   fm.OneObjectTypeName,
		Title:  fm.OneObjectTypeName,
		Fields: make([]*view.FieldModel, 0),
	}
	fm.FModel.Fields = append(fm.FModel.Fields, &view.FieldModel{Field: "id", IsKey: true, Type: "string", IsSearch: false, Visible: false})
	fm.FModel.Fields = append(fm.FModel.Fields, &view.FieldModel{Field: "code", Title: "编码", Type: "string", IsSearch: true, Visible: true, Sorter: true})
	fm.FModel.Fields = append(fm.FModel.Fields, &view.FieldModel{Field: "name", Title: "名称", Type: "string", IsSearch: true, Visible: true, Sorter: true})
	ff := utils.GetPropertyType(model, name)
	if ff == nil {
		logx.Error(oof + "生成外键属性时异常:" + fm.ManyObjectName + "中未找到" + name + "字段,请确认references:中的字段是否正确,大小写必须一致！")
	} else {
		name, _ = getfieldname(ff)
	}
	return name, fm
}
func GetForeignModel(field *view.FieldModel, relevanceModel interface{}, manyFieldKey, display string) *view.ForeignModel {
	fm := &view.ForeignModel{
		IsFkey:             true,
		OneObjectTypeName:  field.Type,
		OneObjectField:     field.Field,
		OneObjectName:      field.Type,
		OneObjectFieldKey:  "id",
		OneDisplayName:     display,
		ManyObjectTypeName: utils.GetTypeName(relevanceModel),
		ManyObjectName:     utils.GetTypeName(relevanceModel),
		ManyObjectField:    field.Field,
		ManyObjectFieldKey: manyFieldKey,
	}
	fm.FModel = &view.ViewModel{
		Name:   fm.OneObjectTypeName,
		Title:  fm.OneObjectTypeName,
		Fields: make([]*view.FieldModel, 0),
	}
	fm.FModel.Fields = append(fm.FModel.Fields, &view.FieldModel{Field: "id", IsKey: true, Type: "string", IsSearch: false, Visible: false})
	fm.FModel.Fields = append(fm.FModel.Fields, &view.FieldModel{Field: "code", Title: "编码", Type: "string", IsSearch: true, Visible: true, Sorter: true})
	fm.FModel.Fields = append(fm.FModel.Fields, &view.FieldModel{Field: "name", Title: "名称", Type: "string", IsSearch: true, Visible: true, Sorter: true})
	return fm
}
func modelToChildModel(model interface{}, mv IManageView) []*view.ViewChildModel {
	childItems := make([]*view.ViewChildModel, 0)
	utils.DeepForItem(model, func(field, parent reflect.StructField, kind utils.TypeKind) {
		if kind == utils.Array {
			child := getChildModel(field, model, mv)
			mv.ViewChildModel(child)
			childItems = append(childItems, child)
		}
	})
	sort.SliceStable(childItems, func(i, j int) bool {
		return childItems[i].Sortindex < childItems[j].Sortindex
	})
	return childItems
}
func getChildModel(field reflect.StructField, model interface{}, mv IManageView) *view.ViewChildModel {
	foreignkey := gormfield(FOREIGNKEY, field)
	vt := field.Type.Elem()
	if vt.Kind() == reflect.Ptr {
		vt = vt.Elem()
	}
	obj := reflect.New(vt).Interface()
	vm := &view.ViewChildModel{
		IsAdd:      true,
		IsEdit:     true,
		IsRemove:   true,
		IsSelect:   false,
		IsCheck:    true,
		ForeignKey: foreignkey,
	}
	vm.ViewModel = *getviewModel(obj, mv)
	for _, f := range vm.ViewModel.Fields {
		if f.IsFieldOrTitle(foreignkey) {
			f.Visible = false
			f.IsSearch = false
		}
		if f.IsFieldOrTitle("CreatedAt") || f.IsFieldOrTitle("UpdatedAt") {
			f.Visible = false
			f.IsSearch = false
		}
	}
	vm.Name = field.Name
	vm.Visible = true
	if field.Name == "ReleaseRecord" {
		vm.Title = "发布记录"
		vm.IsAdd = false
		vm.IsEdit = false
		vm.IsRemove = false
		vm.IsCheck = false
		vm.Sortindex = 100
	}
	mv.ViewChildModel(vm)
	return vm
}
func getviewModel(instance interface{}, mv IManageView) *view.ViewModel {
	vm := &view.ViewModel{}
	vm.Name = utils.GetTypeName(instance)
	vm.Title = vm.Name
	vm.Commands = make([]*view.CommandModel, 0)
	vm.Fields = modelToFiled(instance, mv)
	return vm
}
