package types

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

var symbolMap map[string]string = make(map[string]string)

func init() {
	symbolMap["isnull"] = " is null"
	symbolMap["isnotnull"] = " is not null"
	symbolMap["like"] = "$%s$"
	symbolMap["left"] = "%s$"
	symbolMap["right"] = "$%s"
}

type SearchItem struct {
	Page          int
	Size          int
	Total         int64
	WhereList     []*WhereItem
	SortList      []*SortItem
	Model         interface{}
	IsPreload     bool
	IsStatistical bool   //是否统计
	Statistical   string //统计命令，默认为sum
	StatField     string //统计字段
	groupFields   []string
	db            *gorm.DB
}

func (own *SearchItem) getcol(col string) string {
	if own.db == nil {
		return col
	}
	var namer schema.Namer
	if own.db.NamingStrategy != nil {
		namer = own.db.NamingStrategy
	} else {
		namer = &schema.NamingStrategy{}
	}
	return namer.ColumnName("", col)
}

func (own *SearchItem) Where(db *gorm.DB) (string, []interface{}) {
	w := ""
	own.db = db
	values := make([]interface{}, 0)
	for i, item := range own.WhereList {
		col := own.getcol(item.Column)
		if item.Symbol == "" {
			item.Symbol = "="
		}
		if item.Relation != "" {
			item.Relation = strings.Trim(item.Relation, " ")
		} else {
			if i > 0 {
				item.Relation = " and "
			}
		}
		if v, ok := symbolMap[item.Symbol]; ok {
			if item.Symbol == "isnull" || item.Symbol == "isnotnull" {
				item.Symbol = v
				item.Value = ""
				w = getw(w, col, item, false)
			} else {
				item.Symbol = " like "
				vs := fmt.Sprintf(v, item.Value.(string))
				item.Value = strings.Replace(vs, "$", "%", -1)
				w = getw(w, col, item, true)
				values = append(values, item.Value)
			}
		} else {
			w = getw(w, col, item, true)
			values = append(values, item.Value)
		}
	}
	return w, values
}

func getw(w, col string, item *WhereItem, wh bool) string {
	if w == "" {
		w = item.Prefix + col + item.Symbol
	} else {
		w += " " + item.Relation + " " + item.Prefix + col + item.Symbol
	}
	if wh {
		w += "?"
	}
	w += item.Suffix
	return w
}
func (own *SearchItem) Order() string {
	var order string
	for _, item := range own.SortList {
		col := own.getcol(item.Column)
		if order == "" {
			order = col
		} else {
			order += "," + col
		}
		if item.IsDesc {
			order += " desc"
		}
	}
	if order == "" {
		order = "id desc"
	}
	return order
}
func (own *SearchItem) Group() string {
	var group string
	for _, item := range own.groupFields {
		col := own.getcol(item)
		if group == "" {
			group = col
		} else {
			group += col
		}
	}
	return group
}
func (own *SearchItem) AddGroup(name ...string) *SearchItem {
	if own.groupFields == nil {
		own.groupFields = make([]string, len(name))
	}
	own.groupFields = append(own.groupFields, name...)
	return own
}
func (own *SearchItem) AddWhereN(name string, value interface{}) *SearchItem {
	rel := ""
	if len(own.WhereList) > 0 {
		rel = "AND"
	}
	w := &WhereItem{
		Column:   name,
		Value:    value,
		Relation: rel,
	}
	own.AddWhere(w)
	return own
}
func (own *SearchItem) OrWhereN(name string, value interface{}) *SearchItem {
	rel := ""
	if len(own.WhereList) > 0 {
		rel = "Or"
	}
	w := &WhereItem{
		Column:   name,
		Value:    value,
		Relation: rel,
	}
	if w.Column != "" {
		own.WhereList = append(own.WhereList, w)
	}
	//own.AddWhere(w)
	return own
}
func (own *SearchItem) AddWhere(w ...*WhereItem) *SearchItem {
	if own.WhereList == nil {
		own.WhereList = make([]*WhereItem, 0)
	}
	// for _, item := range w {
	// 	for _, v := range own.WhereList {
	// 		if strings.EqualFold(item.Column, v.Column) {
	// 			return own
	// 		}
	// 	}
	// }
	own.WhereList = append(own.WhereList, w...)
	return own
}

func (own *SearchItem) AddSort(s ...*SortItem) *SearchItem {
	if own.SortList == nil {
		own.SortList = make([]*SortItem, 0)
	}
	for _, item := range s {
		for _, v := range own.SortList {
			if v == nil {
				continue
			}
			if strings.EqualFold(item.Column, v.Column) {
				return own
			}
		}
	}
	if len(s) > 0 {
		own.SortList = append(own.SortList, s...)
	}
	return own
}
func (own *SearchItem) AddSortN(name string, isDesc bool) *SearchItem {
	s := &SortItem{
		Column: name,
		IsDesc: isDesc,
	}
	own.AddSort(s)
	return own
}

type WhereItem struct {
	Column   string
	Value    interface{}
	Symbol   string
	Relation string
	Prefix   string
	Suffix   string
}

type SortItem struct {
	Column string
	IsDesc bool
}
