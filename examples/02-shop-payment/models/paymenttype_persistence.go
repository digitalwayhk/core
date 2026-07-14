package models

import (
	"strings"
	"time"

	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// Insert 校验并写入支付类型。
func (own *PaymentType) Insert() error {
	if err := own.Normalize(); err != nil {
		return err
	}
	own.SetHashcode(own.GetHash())
	return getDataAction().Insert(own)
}

// Update 保存支付类型变化。
func (own *PaymentType) Update() error {
	own.SetUpdatedAt(time.Now().UTC())
	return getDataAction().Update(own)
}

// Delete 删除未被使用的支付类型。
func (own *PaymentType) Delete() error { return getDataAction().Delete(own) }

// FindByID 按 ID 查询支付类型。
func (own *PaymentType) FindByID(id uint) (*PaymentType, error) {
	if err := ensureModel(own); err != nil {
		return nil, err
	}
	var result []*PaymentType
	search := newSearch(own, 1)
	search.AddWhereN("ID", id)
	if err := getDataAction().Load(search, &result); err != nil || len(result) == 0 {
		return nil, err
	}
	return result[0], nil
}

// QueryEnabled 查询启用的支付类型并支持编码、名称筛选。
func (own *PaymentType) QueryEnabled(code, name string) ([]*PaymentType, error) {
	if err := ensureModel(own); err != nil {
		return nil, err
	}
	var result []*PaymentType
	search := newSearch(own, 500)
	search.AddWhereN("Enabled", true)
	if code = strings.TrimSpace(code); code != "" {
		search.AddWhereNS("Code", persistencetypes.SymbolLike, "%"+strings.ToLower(code)+"%")
	}
	if name = strings.TrimSpace(name); name != "" {
		search.AddWhereNS("Name", persistencetypes.SymbolLike, "%"+name+"%")
	}
	err := getDataAction().Load(search, &result)
	return result, err
}

// CodeOrNameExists 检查稳定编码或展示名称是否被其他支付类型占用。
func (own *PaymentType) CodeOrNameExists(code, name string, excludeID uint) (bool, error) {
	if err := ensureModel(own); err != nil {
		return false, err
	}
	var result []*PaymentType
	search := newSearch(own, 500)
	if err := getDataAction().Load(search, &result); err != nil {
		return false, err
	}
	code = strings.ToLower(strings.TrimSpace(code))
	name = strings.TrimSpace(name)
	for _, item := range result {
		if item != nil && item.ID != excludeID && (strings.EqualFold(item.Code, code) || item.Name == name) {
			return true, nil
		}
	}
	return false, nil
}
