package models

import (
	"time"

	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// Insert 写入支付流水及唯一哈希。
func (own *PaymentRecord) Insert() error {
	return own.InsertWith(cloneDataAction())
}

// InsertWith 使用指定事务适配器写入支付流水。
func (own *PaymentRecord) InsertWith(action persistencetypes.IDataAction) error {
	own.NormalizeUserID()
	own.SetHashcode(own.GetHash())
	return action.Insert(own)
}

// Update 保存支付状态变化。
func (own *PaymentRecord) Update() error {
	return own.UpdateWith(cloneDataAction())
}

// UpdateWith 使用指定事务适配器保存支付状态变化。
func (own *PaymentRecord) UpdateWith(action persistencetypes.IDataAction) error {
	own.SetUpdatedAt(time.Now().UTC())
	return action.Update(own)
}

// FindByID 按 ID 查询支付流水。
func (own *PaymentRecord) FindByID(id uint) (*PaymentRecord, error) {
	return own.FindByIDWith(cloneDataAction(), id)
}

// FindByIDWith 使用指定事务适配器按 ID 查询支付流水。
func (own *PaymentRecord) FindByIDWith(action persistencetypes.IDataAction, id uint) (*PaymentRecord, error) {
	if err := ensureModelWith(action, own); err != nil {
		return nil, err
	}
	var result []*PaymentRecord
	search := newSearch(own, 1)
	search.AddWhereN("ID", id)
	if err := action.Load(search, &result); err != nil || len(result) == 0 {
		return nil, err
	}
	return result[0], nil
}

// QueryByOrder 查询订单的全部支付尝试。
func (own *PaymentRecord) QueryByOrder(orderID uint) ([]*PaymentRecord, error) {
	return own.QueryByOrderWith(cloneDataAction(), orderID)
}

// QueryByOrderWith 使用指定事务适配器查询订单的支付尝试。
func (own *PaymentRecord) QueryByOrderWith(action persistencetypes.IDataAction, orderID uint) ([]*PaymentRecord, error) {
	if err := ensureModelWith(action, own); err != nil {
		return nil, err
	}
	var result []*PaymentRecord
	search := newSearch(own, 500)
	search.AddWhereN("OrderID", orderID)
	search.AddSortN("Attempt", false)
	err := action.Load(search, &result)
	return result, err
}

// ExistsByPaymentTypeID 判断支付类型是否已被历史流水引用。
func (own *PaymentRecord) ExistsByPaymentTypeID(paymentTypeID uint) (bool, error) {
	action := cloneDataAction()
	if err := ensureModelWith(action, own); err != nil {
		return false, err
	}
	var result []*PaymentRecord
	search := newSearch(own, 1)
	search.AddWhereN("PaymentTypeID", paymentTypeID)
	if err := action.Load(search, &result); err != nil {
		return false, err
	}
	return len(result) > 0, nil
}

// NextAttempt 返回订单下一次支付尝试序号。
func (own *PaymentRecord) NextAttempt(orderID uint) (int, error) {
	return own.NextAttemptWith(cloneDataAction(), orderID)
}

// NextAttemptWith 使用指定事务适配器计算下一次支付尝试序号。
func (own *PaymentRecord) NextAttemptWith(action persistencetypes.IDataAction, orderID uint) (int, error) {
	items, err := own.QueryByOrderWith(action, orderID)
	if err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 1, nil
	}
	return items[len(items)-1].Attempt + 1, nil
}
