// Package basedata 提供 07 供应商和商品基础资料持久化能力。
package basedata

import (
	"errors"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/supplier-service/models/internal/store"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// SaveSupplierWith 新增或更新供应商资料。
func SaveSupplierWith(action persistencetypes.IDataAction, item *Supplier) error {
	if item == nil {
		return errors.New("供应商不能为空")
	}
	if item.ID == 0 {
		return item.InsertWith(action)
	}
	return item.UpdateWith(action)
}

// ListSuppliers 读取供应商列表。
func ListSuppliers(enabledOnly bool) ([]*Supplier, error) {
	var items []*Supplier
	query := store.NewSearch(NewSupplier(), 1000)
	if enabledOnly {
		query.AddWhereN("Enabled", true)
	}
	query.AddSortN("ID", false)
	err := store.Get().Load(query, &items)
	return items, err
}

// FindSupplierByID 按业务 ID 读取供应商资料。
func FindSupplierByID(id uint) (*Supplier, error) {
	if id == 0 {
		return nil, errors.New("供应商不存在")
	}
	var items []*Supplier
	query := store.NewSearch(NewSupplier(), 1)
	query.AddWhereN("ID", id)
	if err := store.Get().Load(query, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("供应商不存在")
	}
	return items[0], nil
}

// SaveProductWith 新增或更新商品资料。
func SaveProductWith(action persistencetypes.IDataAction, item *Product) error {
	if item == nil {
		return errors.New("商品不能为空")
	}
	if item.ID == 0 {
		return item.InsertWith(action)
	}
	return item.UpdateWith(action)
}

// ListProducts 读取商品列表。
func ListProducts(id uint, enabledOnly bool) ([]*Product, error) {
	var items []*Product
	query := store.NewSearch(NewProduct(), 1000)
	if id > 0 {
		query.AddWhereN("ID", id)
	}
	if enabledOnly {
		query.AddWhereN("Enabled", true)
	}
	query.AddSortN("ID", false)
	err := store.Get().Load(query, &items)
	return items, err
}
