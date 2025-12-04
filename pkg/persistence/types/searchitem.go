package types

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

var symbolMap map[string]string = make(map[string]string)

func init() {
	symbolMap["isnull"] = " IS NULL"
	symbolMap["isnotnull"] = " IS NOT NULL"
	symbolMap["like"] = " LIKE"
	symbolMap["notlike"] = " NOT LIKE"
	symbolMap["left"] = " LIKE"
	symbolMap["right"] = " LIKE"
	symbolMap["in"] = " IN"
	symbolMap["notin"] = " NOT IN"
	symbolMap["between"] = " BETWEEN"
}

type SearchItem struct {
	Page          int
	Size          int
	Total         int64
	WhereList     []*WhereItem
	SortList      []*SortItem
	Model         interface{}
	IsPreload     bool
	IsStatistical bool
	Statistical   string
	StatField     string
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
		if item == nil {
			continue
		}

		col := own.getcol(item.Column)

		// 🔧 标准化符号
		if item.Symbol == "" {
			item.Symbol = "="
		}
		symbolLower := strings.ToLower(strings.TrimSpace(item.Symbol))

		// 🔧 处理关系符号
		if item.Relation != "" {
			item.Relation = strings.ToUpper(strings.Trim(item.Relation, " "))
		} else {
			if i > 0 {
				item.Relation = "AND"
			}
		}

		// 🔧 处理特殊符号
		if mappedSymbol, ok := symbolMap[symbolLower]; ok {
			switch symbolLower {
			case "isnull", "isnotnull":
				// IS NULL / IS NOT NULL 不需要值
				w = own.buildWhereClause(w, col, item, mappedSymbol, false)

			case "like", "notlike":
				// LIKE 查询,值不变
				w = own.buildWhereClause(w, col, item, mappedSymbol, true)
				values = append(values, item.Value)

			case "left":
				// 左模糊: value%
				w = own.buildWhereClause(w, col, item, " LIKE", true)
				values = append(values, fmt.Sprintf("%s%%", item.Value))

			case "right":
				// 右模糊: %value
				w = own.buildWhereClause(w, col, item, " LIKE", true)
				values = append(values, fmt.Sprintf("%%s", item.Value))

			case "in", "notin":
				// IN / NOT IN 查询
				w = own.buildWhereClause(w, col, item, mappedSymbol, true)
				values = append(values, item.Value)

			case "between":
				// BETWEEN 查询
				w = own.buildBetweenClause(w, col, item)
				if betweenValues, ok := item.Value.([]interface{}); ok && len(betweenValues) == 2 {
					values = append(values, betweenValues[0], betweenValues[1])
				}

			default:
				w = own.buildWhereClause(w, col, item, mappedSymbol, true)
				values = append(values, item.Value)
			}
		} else {
			// 🔧 普通符号: =, !=, >, <, >=, <=
			w = own.buildWhereClause(w, col, item, " "+strings.ToUpper(item.Symbol), true)
			values = append(values, item.Value)
		}
	}

	return w, values
}

// 🔧 新增：构建 WHERE 子句
func (own *SearchItem) buildWhereClause(w, col string, item *WhereItem, symbol string, needPlaceholder bool) string {
	if w == "" {
		w = item.Prefix + col + symbol
	} else {
		w += " " + item.Relation + " " + item.Prefix + col + symbol
	}

	if needPlaceholder {
		w += " ?"
	}

	w += item.Suffix
	return w
}

// 🔧 新增：构建 BETWEEN 子句
func (own *SearchItem) buildBetweenClause(w, col string, item *WhereItem) string {
	if w == "" {
		w = item.Prefix + col + " BETWEEN ? AND ?"
	} else {
		w += " " + item.Relation + " " + item.Prefix + col + " BETWEEN ? AND ?"
	}
	w += item.Suffix
	return w
}

func (own *SearchItem) Order() string {
	var order string
	for _, item := range own.SortList {
		if item == nil {
			continue
		}
		col := own.getcol(item.Column)
		if order == "" {
			order = col
		} else {
			order += "," + col
		}
		if item.IsDesc {
			order += " DESC"
		}
	}
	if order == "" {
		order = "id DESC"
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
			group += "," + col
		}
	}
	return group
}

func (own *SearchItem) AddGroup(name ...string) *SearchItem {
	if own.groupFields == nil {
		own.groupFields = make([]string, 0, len(name))
	}
	own.groupFields = append(own.groupFields, name...)
	return own
}

func (own *SearchItem) AddWhereN(name string, value interface{}) *WhereItem {
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
	return w
}

func (own *SearchItem) AddWhereNS(name string, symbol string, value interface{}) *WhereItem {
	rel := ""
	if len(own.WhereList) > 0 {
		rel = "AND"
	}
	w := &WhereItem{
		Column:   name,
		Value:    value,
		Symbol:   symbol,
		Relation: rel,
	}
	own.AddWhere(w)
	return w
}

func (own *SearchItem) OrWhereN(name string, value interface{}) *WhereItem {
	rel := ""
	if len(own.WhereList) > 0 {
		rel = "OR"
	}
	w := &WhereItem{
		Column:   name,
		Value:    value,
		Relation: rel,
	}
	if w.Column != "" {
		own.WhereList = append(own.WhereList, w)
	}
	return w
}

func (own *SearchItem) AddWhere(w ...*WhereItem) *SearchItem {
	if own.WhereList == nil {
		own.WhereList = make([]*WhereItem, 0)
	}
	own.WhereList = append(own.WhereList, w...)
	return own
}

func (own *SearchItem) AddSort(s ...*SortItem) *SearchItem {
	if own.SortList == nil {
		own.SortList = make([]*SortItem, 0)
	}
	for _, item := range s {
		if item == nil {
			continue
		}
		// 检查重复
		isDuplicate := false
		for _, v := range own.SortList {
			if v == nil {
				continue
			}
			if strings.EqualFold(item.Column, v.Column) {
				isDuplicate = true
				break
			}
		}
		if !isDuplicate {
			own.SortList = append(own.SortList, item)
		}
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

func (own *WhereItem) Start() *WhereItem {
	own.Prefix = "("
	return own
}

func (own *WhereItem) And() *WhereItem {
	own.Relation = "AND"
	return own
}

func (own *WhereItem) Or() *WhereItem {
	own.Relation = "OR"
	return own
}

func (own *WhereItem) End() *WhereItem {
	own.Suffix = ")"
	return own
}

func (own *WhereItem) Like() *WhereItem {
	own.Symbol = "like"
	return own
}

func (own *WhereItem) NotLike() *WhereItem {
	own.Symbol = "notlike"
	return own
}

func (own *WhereItem) Not() *WhereItem {
	own.Symbol = "!="
	return own
}

func (own *WhereItem) IsNull() *WhereItem {
	own.Symbol = "isnull"
	return own
}

func (own *WhereItem) IsNotNull() *WhereItem {
	own.Symbol = "isnotnull"
	return own
}

func (own *WhereItem) In() *WhereItem {
	own.Symbol = "in"
	return own
}

func (own *WhereItem) NotIn() *WhereItem {
	own.Symbol = "notin"
	return own
}

func (own *WhereItem) Between() *WhereItem {
	own.Symbol = "between"
	return own
}

func (own *WhereItem) SetColumn(name string) *WhereItem {
	own.Column = name
	return own
}

func (own *WhereItem) SetValue(value interface{}) *WhereItem {
	own.Value = value
	return own
}

func (own *WhereItem) SetSymbol(symbol string) *WhereItem {
	own.Symbol = symbol
	return own
}

type SortItem struct {
	Column string
	IsDesc bool
}
