package models

import (
	"strings"

	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// Query 直接通过模型数据适配器按可选 ID 和名称组合查询商品。
func (own *Product) Query(action persistencetypes.IDataAction, id uint, name string) ([]*Product, error) {
	if err := requireDataAction(action); err != nil {
		return nil, err
	}
	search := newProductSearch(own, 500)
	if id > 0 {
		search.AddWhereN("ID", id)
	}
	if name = strings.TrimSpace(name); name != "" {
		search.AddWhereNS("Name", persistencetypes.SymbolLike, "%"+name+"%")
	}
	var products []*Product
	err := action.Load(search, &products)
	return products, err
}

// FindByID 直接通过模型数据适配器查找单个商品。
func (own *Product) FindByID(action persistencetypes.IDataAction, id uint) (*Product, error) {
	if err := requireDataAction(action); err != nil {
		return nil, err
	}
	search := newProductSearch(own, 1)
	search.AddWhereN("ID", id)
	var products []*Product
	if err := action.Load(search, &products); err != nil {
		return nil, err
	}
	if len(products) == 0 {
		return nil, nil
	}
	return products[0], nil
}

// NameExists 检查规范化名称是否已被其他商品占用。
func (own *Product) NameExists(action persistencetypes.IDataAction, name string, excludeID uint) (bool, error) {
	if err := requireDataAction(action); err != nil {
		return false, err
	}
	search := newProductSearch(own, 2)
	productWithName := NewProduct()
	productWithName.Name = strings.TrimSpace(name)
	search.AddWhereN("Hashcode", productWithName.GetHash())
	var products []*Product
	if err := action.Load(search, &products); err != nil {
		return false, err
	}
	for _, product := range products {
		if product != nil && product.ID != excludeID {
			return true, nil
		}
	}
	return false, nil
}

// newProductSearch 创建模型直接查询所需的统一 SearchItem。
func newProductSearch(model *Product, size int) *persistencetypes.SearchItem {
	return &persistencetypes.SearchItem{Page: 1, Size: size, Model: model}
}
