package types

import "gorm.io/gorm"

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
