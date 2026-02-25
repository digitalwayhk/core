package view

import (
	"reflect"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/zeromicro/go-zero/core/logx"
)

type ViewModel struct {
	*entity.Model `json:"-"`
	Name          string            `json:"name"`
	ServiceName   string            `json:"servicename"`
	Title         string            `json:"title"`
	Disabled      bool              `json:"disabled"`
	Visible       bool              `json:"visible"`
	ViewType      string            `json:"viewtype"`
	AutoLoad      bool              `json:"autoload"`
	Fields        []*FieldModel     `json:"fields"`
	Commands      []*CommandModel   `json:"commands"`
	ChildModels   []*ViewChildModel `json:"childmodels"`
	ShowComvtp    bool              `json:"showComvtp"` //是否显示左边工具栏
	AutoSearch    bool              `json:"autoSearch"` //左边工具栏是否自动搜索
}

func (own *ViewModel) ViewField(name string) *FieldModel {
	for _, f := range own.Fields {
		if strings.EqualFold(f.Field, name) {
			return f
		}
		if strings.EqualFold(f.PropField, name) {
			return f
		}
	}
	return nil
}
func (own *ViewModel) GetHash() string {
	return utils.HashCodes(own.Name)
}

type ViewChildModel struct {
	*entity.Model `json:"-"`
	ViewModel
	IsAdd      bool   `json:"isadd"`
	IsEdit     bool   `json:"isedit"`
	IsRemove   bool   `json:"isremove"`
	IsSelect   bool   `json:"isselect"`
	IsCheck    bool   `json:"ischeck"`
	ForeignKey string `json:"foreignKey"`
	Sortindex  int    `json:"sortindex"`
	References string `json:"references"`
}

type CommandModel struct {
	*entity.Model  `json:"-"`
	Command        string `json:"command"`
	Name           string `json:"name"`
	Title          string `json:"title"`
	IsSelectRow    bool   `json:"isselectrow"`
	SelectMultiple bool   `json:"selectmultiple"`
	IsAlert        bool   `json:"isalert"`
	EditShow       bool   `json:"editshow"`
	IsSplit        bool   `json:"issplit"`
	SplitName      string `json:"splitname"`
	Disabled       bool   `json:"disabled"`
	Visible        bool   `json:"visible"`
	OnClick        string `json:"onclick"`
	Icon           string `json:"icon"`
	Index          int    `json:"index"`
}

type FieldModel struct {
	*entity.Model `json:"-"`
	IsKey         bool              `json:"iskey"`
	Field         string            `json:"field"`     // 字段名称，有可能是json中的属性名称，当为json时，可能与数据库字段名称不同
	PropField     string            `json:"porpfield"` // 属性字段名称，当field和本字段不同时，使用此字段作为属性名称
	Title         string            `json:"title"`
	Index         int               `json:"index"`
	Disabled      bool              `json:"disabled"`
	Visible       bool              `json:"visible"`
	IsEdit        bool              `json:"isedit"`
	Type          string            `json:"type"`
	DataTimeType  DataTimeTypeModel `json:"datatimetype"`
	Required      bool              `json:"required"`
	Length        int               `json:"length"`
	Precision     int               `json:"precision"`
	Min           int               `json:"min"`
	IsSearch      bool              `json:"issearch"`
	IsRemark      bool              `json:"isremark"`
	IsPassword    bool              `json:"ispassword"`
	Sorter        bool              `json:"sorter"`
	Tag           interface{}       `json:"tag"`
	DefaultValue  interface{}       `json:"defaultvalue"`
	ComVtp        *ComBoxModel      `json:"comvtp"`
	Foreign       *ForeignModel     `json:"foreign"`
	PostType      string            `json:"posttype"`
	FieldType     reflect.Type      `json:"-"`
	ShowInComvtp  bool              `json:"showInComvtp"`
}
type DataTimeTypeModel struct {
	IsDate     bool   `json:"isdate"`
	IsTime     bool   `json:"istime"`
	DateFormat string `json:"dateformat"` // YYYY-MM-dd HH:mm:ss
	TimeFormat string `json:"timeformat"` // HH:mm:ss
	IsUTC      bool   `json:"isutc"`
}

func (own *DataTimeTypeModel) SetDate(is bool) {
	own.IsDate = is
	own.DateFormat = ""
	if own.IsDate {
		own.DateFormat = "YYYY-MM-DD"
	}
	// if own.IsTime {
	// 	own.DateFormat = "YYYY-MM-DD HH:mm:ss"
	// }
}
func (own *DataTimeTypeModel) SetTime(is bool) {
	own.IsTime = is
	own.TimeFormat = ""
	if own.IsTime {
		own.TimeFormat = "HH:mm:ss"
	}
	// if own.IsDate {
	// 	own.DateFormat = "YYYY-MM-DD HH:mm:ss"
	// }
}
func (own *FieldModel) ComBox(values ...string) {
	for i, v := range values {
		own.ComBoxValue(i, v)
	}
}
func (own *FieldModel) ComBoxValue(key int, value string) {
	if own.ComVtp == nil {
		own.ComVtp = &ComBoxModel{
			Isvtp:    true,
			Multiple: false,
			Items:    make(map[int]string),
		}
	}
	own.ComVtp.Items[key] = value
}
func (own *FieldModel) IsFieldOrTitle(name ...string) bool {
	if len(name) <= 0 {
		return false
	}
	for _, n := range name {
		if strings.EqualFold(own.Field, n) {
			return true
		}
		if strings.EqualFold(own.PropField, n) {
			return true
		}
	}
	for _, n := range name {
		if strings.EqualFold(own.Title, n) {
			return true
		}
	}
	return false
}

type ComBoxModel struct {
	*entity.Model `json:"-"`
	Isvtp         bool           `json:"isvtp"`
	Multiple      bool           `json:"multiple"`
	Items         map[int]string `json:"items"`
}
type ForeignModel struct {
	*entity.Model            `json:"-"`
	IsFkey                   bool              `json:"isfkey"`
	OneObjectTypeName        string            `json:"oneobjecttypename"`
	OneObjectName            string            `json:"oneobjectname"`
	OneObjectField           string            `json:"oneobjectfield"`    //one对象中的many对象的field名称
	OneObjectFieldKey        string            `json:"oneobjectfieldkey"` //one对象中的many对象的关联字段名称
	OneDisplayName           string            `json:"onedisplayname"`
	OneObjectForeignKeyValue string            `json:"oneobjectforeignkeyvalue"`
	ManyObjectTypeName       string            `json:"manyobjecttypename"`
	ManyObjectName           string            `json:"manyobjectname"`
	ManyObjectField          string            `json:"manyobjectfield"`
	ManyObjectFieldKey       string            `json:"manyobjectfieldkey"`
	ManyDisplayField         string            `json:"manydisplayfield"`
	MapItems                 map[string]string `json:"mapitems"`
	FModel                   *ViewModel        `json:"model"`
}

type SearchWhere struct {
	Name   string      `json:"name"`
	Symbol string      `json:"symbol"`
	Value  interface{} `json:"value"`
}
type SearchSort struct {
	Name   string `json:"name"`
	Isdesc bool   `json:"isdesc"`
}
type SearchItem struct {
	Field      *FieldModel     `json:"field"`
	Foreign    *ForeignModel   `json:"foreign"`
	Parent     interface{}     `json:"parent"`
	ChildModel *ViewChildModel `json:"childmodel"`
	Page       int             `json:"page"`
	Size       int             `json:"size"`
	WhereList  []*SearchWhere  `json:"whereList"`
	SortList   []*SearchSort   `json:"sortList"`
	Value      string          `json:"value"`
	View       *ViewModel      `json:"-"`
	Tag        string          `json:"tag"` // 扩展字段,通过TableData传递到前端，前端不处理，在这里返回
}

func (own *SearchItem) ToSearchItem() *types.SearchItem {
	item := &types.SearchItem{}
	vsTops(own, item)
	if own.View != nil {
		for _, w := range item.WhereList {
			field := own.View.ViewField(w.Column)
			if field != nil {
				if field.Type != utils.GetTypeName(w.Value) {
					if field.Type == "datetime" {
						layout := ""
						if field.DataTimeType.IsDate {
							layout = "2006-01-02"
						}
						if field.DataTimeType.IsTime {
							if layout == "" {
								layout = " 15:04:05"
							} else {
								layout += " 15:04:05"
							}

						}
						val, err := time.Parse(layout, strings.Trim(reflect.ValueOf(w.Value).String(), " "))
						if err != nil {
							logx.Error(err)
						}
						w.Value = val
						continue
					}
					w.Value, _ = utils.AnyToTypeData(w.Value, field.FieldType)
				}
			}
		}
	}
	return item
}

type TableData struct {
	Rows  interface{} `json:"rows"`
	Total int64       `json:"total"`
	Tag   string      `json:"tag"` // 扩展字段,传递到前端，前端不处理，在SearchItem中返回
}
type ForeigData struct {
	Rows  interface{} `json:"rows"`
	Total int64       `json:"total"`
	Model *ViewModel  `json:"model"`
}

func vsTops(vs *SearchItem, ps *types.SearchItem) {
	ps.Page = vs.Page
	ps.Size = vs.Size
	for _, w := range vs.WhereList {
		ps.AddWhere(&types.WhereItem{
			Column: w.Name,
			Value:  w.Value,
			Symbol: w.Symbol,
		})
	}
	for _, s := range vs.SortList {
		ps.AddSort(&types.SortItem{
			Column: s.Name,
			IsDesc: s.Isdesc,
		})
	}
}
