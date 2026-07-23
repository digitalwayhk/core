package basedata

import (
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models/internal/store"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// Insert 校验并写入商品。
func (own *Product) Insert() error {
	if err := own.validate(0); err != nil {
		return err
	}
	own.SetHashcode(own.GetHash())
	return store.Get().Insert(own)
}

// Update 保存商品变化。
func (own *Product) Update() error {
	own.SetUpdatedAt(time.Now().UTC())
	return store.Get().Update(own)
}

// Delete 删除没有订单引用的商品。
func (own *Product) Delete() error { return store.Get().Delete(own) }

// FindByID 按 ID 查询商品。
func (own *Product) FindByID(id uint) (*Product, error) {
	return own.FindByIDWith(store.Get(), id)
}

// FindByIDWith 使用指定数据操作器查询商品。
func (own *Product) FindByIDWith(action persistencetypes.IDataAction, id uint) (*Product, error) {
	if err := store.EnsureModelWith(action, own); err != nil {
		return nil, err
	}
	var result []*Product
	search := store.NewSearch(own, 1)
	search.AddWhereN("ID", id)
	if err := action.Load(search, &result); err != nil || len(result) == 0 {
		return nil, err
	}
	return result[0], nil
}

// Query 按可选条件查询全部商品，供管理和业务校验使用。
func (own *Product) Query(id uint, code, name string, supplierID uint) ([]*Product, error) {
	if err := store.EnsureModel(own); err != nil {
		return nil, err
	}
	search := store.NewSearch(own, 500)
	if id > 0 {
		search.AddWhereN("ID", id)
	}
	if code = strings.TrimSpace(code); code != "" {
		search.AddWhereNS("Code", persistencetypes.SymbolLike, "%"+strings.ToLower(code)+"%")
	}
	if name = strings.TrimSpace(name); name != "" {
		search.AddWhereNS("Name", persistencetypes.SymbolLike, "%"+name+"%")
	}
	if supplierID > 0 {
		search.AddWhereN("SupplierID", supplierID)
	}
	var result []*Product
	err := store.Get().Load(search, &result)
	return result, err
}

// QueryAvailable 查询商品和供应商均启用的公开商品。
func (own *Product) QueryAvailable(id uint, code, name string, supplierID uint, supplierCode string) ([]*Product, error) {
	products, err := own.Query(id, code, name, supplierID)
	if err != nil {
		return nil, err
	}
	supplierCode = strings.ToLower(strings.TrimSpace(supplierCode))
	result := make([]*Product, 0, len(products))
	for _, product := range products {
		if product == nil || !product.Enabled {
			continue
		}
		supplier, findErr := NewSupplier().FindByID(product.SupplierID)
		if findErr != nil {
			return nil, findErr
		}
		if supplier == nil || !supplier.Enabled {
			continue
		}
		if supplierCode != "" && !strings.Contains(strings.ToLower(supplier.Code), supplierCode) {
			continue
		}
		product.Supplier = supplier
		result = append(result, product)
	}
	return result, nil
}

// CodeOrNameExists 检查商品编码或名称占用。
func (own *Product) CodeOrNameExists(code, name string, excludeID uint) (bool, error) {
	return codeOrNameExists(own, code, name, excludeID)
}
