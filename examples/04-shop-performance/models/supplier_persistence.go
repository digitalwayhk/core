package models

import (
	"strings"
	"time"

	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// Insert 校验并写入供应商。
func (own *Supplier) Insert() error {
	if err := own.validate(0); err != nil {
		return err
	}
	own.SetHashcode(own.GetHash())
	return cloneDataAction().Insert(own)
}

// Update 保存供应商变化。
func (own *Supplier) Update() error {
	own.SetUpdatedAt(time.Now().UTC())
	return cloneDataAction().Update(own)
}

// Delete 删除没有商品引用的供应商。
func (own *Supplier) Delete() error { return cloneDataAction().Delete(own) }

// FindByID 按 ID 查询供应商。
func (own *Supplier) FindByID(id uint) (*Supplier, error) {
	return own.FindByIDWith(cloneDataAction(), id)
}

// FindByIDWith 使用指定数据操作器查询供应商。
func (own *Supplier) FindByIDWith(action persistencetypes.IDataAction, id uint) (*Supplier, error) {
	if err := ensureModelWith(action, own); err != nil {
		return nil, err
	}
	var result []*Supplier
	search := newSearch(own, 1)
	search.AddWhereN("ID", id)
	if err := action.Load(search, &result); err != nil || len(result) == 0 {
		return nil, err
	}
	return result[0], nil
}

// QueryEnabled 查询启用供应商并支持可选筛选。
func (own *Supplier) QueryEnabled(id uint, code, name string) ([]*Supplier, error) {
	action := cloneDataAction()
	if err := ensureModelWith(action, own); err != nil {
		return nil, err
	}
	search := newSearch(own, 500)
	search.AddWhereN("Enabled", true)
	if id > 0 {
		search.AddWhereN("ID", id)
	}
	if code = strings.TrimSpace(code); code != "" {
		search.AddWhereNS("Code", persistencetypes.SymbolLike, "%"+strings.ToLower(code)+"%")
	}
	if name = strings.TrimSpace(name); name != "" {
		search.AddWhereNS("Name", persistencetypes.SymbolLike, "%"+name+"%")
	}
	var result []*Supplier
	err := action.Load(search, &result)
	return result, err
}

// CodeOrNameExists 检查供应商编码或名称占用。
func (own *Supplier) CodeOrNameExists(code, name string, excludeID uint) (bool, error) {
	return codeOrNameExists(own, code, name, excludeID)
}

// HasProducts 判断供应商是否存在商品引用。
func (own *Supplier) HasProducts(id uint) (bool, error) {
	action := cloneDataAction()
	if err := ensureModelWith(action, NewProduct()); err != nil {
		return false, err
	}
	search := newSearch(NewProduct(), 1)
	search.AddWhereN("SupplierID", id)
	var products []*Product
	if err := action.Load(search, &products); err != nil {
		return false, err
	}
	return len(products) > 0, nil
}
