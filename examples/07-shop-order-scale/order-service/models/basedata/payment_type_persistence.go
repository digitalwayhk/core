// Package basedata 提供 07 订单服务支付类型远程权威库访问能力。
package basedata

import (
	"errors"
	"strings"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/internal/store"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// ListPaymentTypesWith 从远程权威库读取支付类型列表。
func ListPaymentTypesWith(action persistencetypes.IDataAction, enabledOnly bool) ([]*PaymentType, error) {
	var items []*PaymentType
	query := store.NewSearch(NewPaymentType(), 1000)
	if enabledOnly {
		query.AddWhereN("Enabled", true)
	}
	query.AddSortN("ID", false)
	err := action.Load(query, &items)
	return items, err
}

// FindPaymentTypeWith 按 ID 从远程权威库读取支付类型。
func FindPaymentTypeWith(action persistencetypes.IDataAction, id uint) (*PaymentType, error) {
	var items []*PaymentType
	query := store.NewSearch(NewPaymentType(), 1)
	query.AddWhereN("ID", id)
	if err := action.Load(query, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("支付类型不存在")
	}
	return items[0], nil
}

// SavePaymentTypeWith 在远程权威库新增或更新支付类型。
func SavePaymentTypeWith(action persistencetypes.IDataAction, item *PaymentType) error {
	if item == nil {
		return errors.New("支付类型不能为空")
	}
	existing, err := findPaymentTypeByCodeWith(action, item.Code)
	if err != nil {
		return item.InsertWith(action)
	}
	item.ID = existing.ID
	return item.UpdateWith(action)
}

func findPaymentTypeByCodeWith(action persistencetypes.IDataAction, code string) (*PaymentType, error) {
	var items []*PaymentType
	query := store.NewSearch(NewPaymentType(), 1)
	query.AddWhereN("Code", strings.TrimSpace(code))
	if err := action.Load(query, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("支付类型不存在")
	}
	return items[0], nil
}
