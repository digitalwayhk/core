package models

import (
	"strings"
	"time"

	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// Insert 校验并写入支付类型。
func (own *PaymentType) Insert() error {
	if err := own.validate(0); err != nil {
		return err
	}
	own.SetHashcode(own.GetHash())
	return cloneDataAction().Insert(own)
}

// Update 保存支付类型变化。
func (own *PaymentType) Update() error {
	own.SetUpdatedAt(time.Now().UTC())
	return cloneDataAction().Update(own)
}

// Delete 删除未被使用的支付类型。
func (own *PaymentType) Delete() error { return cloneDataAction().Delete(own) }

// FindByID 按 ID 查询支付类型。
func (own *PaymentType) FindByID(id uint) (*PaymentType, error) {
	return own.FindByIDWith(cloneDataAction(), id)
}

// FindByIDWith 使用指定数据操作器查询支付类型。
func (own *PaymentType) FindByIDWith(action persistencetypes.IDataAction, id uint) (*PaymentType, error) {
	if err := ensureModelWith(action, own); err != nil {
		return nil, err
	}
	var result []*PaymentType
	search := newSearch(own, 1)
	search.AddWhereN("ID", id)
	if err := action.Load(search, &result); err != nil || len(result) == 0 {
		return nil, err
	}
	return result[0], nil
}

// QueryEnabled 查询启用支付类型并支持可选筛选。
func (own *PaymentType) QueryEnabled(code, name string) ([]*PaymentType, error) {
	action := cloneDataAction()
	if err := ensureModelWith(action, own); err != nil {
		return nil, err
	}
	search := newSearch(own, 500)
	search.AddWhereN("Enabled", true)
	if code = strings.TrimSpace(code); code != "" {
		search.AddWhereNS("Code", persistencetypes.SymbolLike, "%"+strings.ToLower(code)+"%")
	}
	if name = strings.TrimSpace(name); name != "" {
		search.AddWhereNS("Name", persistencetypes.SymbolLike, "%"+name+"%")
	}
	var result []*PaymentType
	err := action.Load(search, &result)
	return result, err
}

// CodeOrNameExists 检查支付类型编码或名称占用。
func (own *PaymentType) CodeOrNameExists(code, name string, excludeID uint) (bool, error) {
	return codeOrNameExists(own, code, name, excludeID)
}
