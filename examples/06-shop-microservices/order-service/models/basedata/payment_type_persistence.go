// 本文件定义当前服务基础资料模型及其持久化能力。
package basedata

import (
	"errors"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/internal/store"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/transaction"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// ListPaymentTypes 执行本文件能力对应的业务操作。
func ListPaymentTypes(enabledOnly bool) ([]*PaymentType, error) {
	if err := store.EnsureModel(NewPaymentType()); err != nil {
		return nil, err
	}
	var items []*PaymentType
	query := store.NewSearch(NewPaymentType(), 100)
	if enabledOnly {
		query.AddWhereN("Enabled", true)
	}
	err := store.Get().Load(query, &items)
	return items, err
}

// FindPaymentType 执行本文件能力对应的业务操作。
func FindPaymentType(id uint) (*PaymentType, error) {
	return FindPaymentTypeWith(store.Get(), id)
}

// FindPaymentTypeWith 执行本文件能力对应的业务操作。
func FindPaymentTypeWith(action persistencetypes.IDataAction, id uint) (*PaymentType, error) {
	var items []*PaymentType
	query := store.NewSearch(NewPaymentType(), 1)
	query.AddWhereN("ID", id)
	if err := action.Load(query, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

// InsertWith 实现本类型在当前服务边界中的行为。
func (p *PaymentType) InsertWith(action persistencetypes.IDataAction) error {
	p.Name, p.Code = strings.TrimSpace(p.Name), strings.ToLower(strings.TrimSpace(p.Code))
	if p.Name == "" || p.Code == "" {
		return errors.New("支付类型名称和编码不能为空")
	}
	p.SetHashcode(p.GetHash())
	return action.Insert(p)
}

// UpdateWith 实现本类型在当前服务边界中的行为。
func (p *PaymentType) UpdateWith(action persistencetypes.IDataAction) error {
	p.Name, p.Code = strings.TrimSpace(p.Name), strings.ToLower(strings.TrimSpace(p.Code))
	if p.Name == "" || p.Code == "" {
		return errors.New("支付类型名称和编码不能为空")
	}
	p.SetHashcode(p.GetHash())
	p.SetUpdatedAt(time.Now().UTC())
	return action.Update(p)
}

// DeleteWith 实现本类型在当前服务边界中的行为。
func (p *PaymentType) DeleteWith(action persistencetypes.IDataAction) error { return action.Delete(p) }

// PaymentTypeInUse 执行本文件能力对应的业务操作。
func PaymentTypeInUse(id uint) (bool, error) {
	return PaymentTypeInUseWith(store.Get(), id)
}

// PaymentTypeInUseWith 执行本文件能力对应的业务操作。
func PaymentTypeInUseWith(action persistencetypes.IDataAction, id uint) (bool, error) {
	var items []*transaction.PaymentRecord
	query := store.NewSearch(transaction.NewPaymentRecord(), 1)
	query.AddWhereN("PaymentTypeID", id)
	if err := action.Load(query, &items); err != nil {
		return false, err
	}
	return len(items) > 0, nil
}

// SavePaymentType 执行本文件能力对应的业务操作。
func SavePaymentType(item *PaymentType) error {
	item.Name = strings.TrimSpace(item.Name)
	item.Code = strings.ToLower(strings.TrimSpace(item.Code))
	if item.Name == "" || item.Code == "" {
		return errors.New("支付类型名称和编码不能为空")
	}
	if item.ID != 0 {
		if old, err := FindPaymentType(item.ID); err == nil && old != nil && old.Code != item.Code {
			used, useErr := PaymentTypeInUse(item.ID)
			if useErr != nil {
				return useErr
			}
			if used {
				return contract.ErrResourceInUse
			}
		}
	}
	item.SetHashcode(item.GetHash())
	if item.CreatedAt == nil {
		return store.Get().Insert(item)
	}
	item.SetUpdatedAt(time.Now().UTC())
	return store.Get().Update(item)
}

// DeletePaymentType 执行本文件能力对应的业务操作。
func DeletePaymentType(item *PaymentType) error {
	used, err := PaymentTypeInUse(item.ID)
	if err != nil {
		return err
	}
	if used {
		return contract.ErrResourceInUse
	}
	return store.Get().Delete(item)
}
