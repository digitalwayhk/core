package models

import (
	"strings"

	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// Insert 写入商品并保存业务唯一哈希。
func (own *Product) Insert() error {
	if err := own.validate(); err != nil {
		return err
	}
	own.SetHashcode(own.GetHash())
	return getDataAction().Insert(own)
}

// Query 按可选 ID 和名称查询商品。
func (own *Product) Query(id uint, name string) ([]*Product, error) {
	if err := ensureModel(own); err != nil {
		return nil, err
	}
	search := newSearch(own, 500)
	if id > 0 {
		search.AddWhereN("ID", id)
	}
	if name = strings.TrimSpace(name); name != "" {
		search.AddWhereNS("Name", persistencetypes.SymbolLike, "%"+name+"%")
	}
	var result []*Product
	err := getDataAction().Load(search, &result)
	return result, err
}

// FindByID 按 ID 查找商品。
func (own *Product) FindByID(id uint) (*Product, error) {
	if err := ensureModel(own); err != nil {
		return nil, err
	}
	var result []*Product
	search := newSearch(own, 1)
	search.AddWhereN("ID", id)
	if err := getDataAction().Load(search, &result); err != nil || len(result) == 0 {
		return nil, err
	}
	return result[0], nil
}

// NameExists 检查商品名称是否被其他记录占用。
func (own *Product) NameExists(name string, excludeID uint) (bool, error) {
	if err := ensureModel(own); err != nil {
		return false, err
	}
	candidate := NewProduct()
	candidate.Name = name
	var result []*Product
	search := newSearch(own, 2)
	search.AddWhereN("Hashcode", candidate.GetHash())
	if err := getDataAction().Load(search, &result); err != nil {
		return false, err
	}
	for _, item := range result {
		if item != nil && item.ID != excludeID {
			return true, nil
		}
	}
	return false, nil
}

// newSearch 创建模型直接查询使用的统一分页条件。
func newSearch(model interface{}, size int) *persistencetypes.SearchItem {
	return &persistencetypes.SearchItem{Page: 1, Size: size, Model: model}
}
