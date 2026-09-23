package models

import (
	"strings"

	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// Query 按可选 ID 和名称组合查询分类。
func (own *Category) Query(id uint, name string) ([]*Category, error) {
	search := newCategorySearch(own, 500)
	if id > 0 {
		search.AddWhereN("ID", id)
	}
	if name = strings.TrimSpace(name); name != "" {
		search.AddWhereNS("Name", persistencetypes.SymbolLike, "%"+name+"%")
	}
	var items []*Category
	err := getDataAction().Load(search, &items)
	return items, err
}

// FindByID 查找单个分类。
func (own *Category) FindByID(id uint) (*Category, error) {
	if id == 0 {
		return nil, nil
	}
	search := newCategorySearch(own, 1)
	search.AddWhereN("ID", id)
	var items []*Category
	if err := getDataAction().Load(search, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	return items[0], nil
}

// FindByCode 按编码查找分类。
func (own *Category) FindByCode(code string) (*Category, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	if code == "" {
		return nil, nil
	}
	search := newCategorySearch(own, 1)
	search.AddWhereN("Code", code)
	var items []*Category
	if err := getDataAction().Load(search, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	return items[0], nil
}

// CodeOrNameExists 检查编码或名称是否已被其他分类占用。
func (own *Category) CodeOrNameExists(code, name string, excludeID uint) (bool, error) {
	search := newCategorySearch(own, 8)
	hashed := NewCategory()
	hashed.Code = code
	search.AddWhereN("Hashcode", hashed.GetHash())
	var items []*Category
	if err := getDataAction().Load(search, &items); err != nil {
		return false, err
	}
	for _, item := range items {
		if item != nil && item.ID != excludeID {
			return true, nil
		}
	}
	nameSearch := newCategorySearch(own, 8)
	nameSearch.AddWhereN("Name", strings.TrimSpace(name))
	var named []*Category
	if err := getDataAction().Load(nameSearch, &named); err != nil {
		return false, err
	}
	for _, item := range named {
		if item != nil && item.ID != excludeID {
			return true, nil
		}
	}
	return false, nil
}

// DefaultCategories 返回分类管理首次空表时写入的演示数据。
func DefaultCategories() []*Category {
	return []*Category{
		newSeedCategory("electronics", "电子", KindElectronics, "用于验证枚举分段筛选的电子分类"),
		newSeedCategory("books", "图书", KindBook, "用于验证枚举分段筛选的图书分类"),
		newSeedCategory("accessories", "配件", KindAccessory, "用于验证枚举分段筛选的配件分类"),
	}
}

func newSeedCategory(code, name string, kind int, describe string) *Category {
	item := NewCategory()
	item.Code = code
	item.Name = name
	item.Kind = kind
	item.Enabled = true
	item.Describe = describe
	return item
}

func newCategorySearch(model *Category, size int) *persistencetypes.SearchItem {
	return &persistencetypes.SearchItem{Page: 1, Size: size, Model: model}
}
