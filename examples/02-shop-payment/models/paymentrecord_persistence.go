package models

import "time"

// Insert 写入支付流水及唯一哈希。
func (own *PaymentRecord) Insert() error {
	own.NormalizeUserID()
	own.SetHashcode(own.GetHash())
	return getDataAction().Insert(own)
}

// Update 保存支付状态变化。
func (own *PaymentRecord) Update() error {
	own.SetUpdatedAt(time.Now().UTC())
	return getDataAction().Update(own)
}

// FindByID 按 ID 查询支付流水。
func (own *PaymentRecord) FindByID(id uint) (*PaymentRecord, error) {
	if err := ensureModel(own); err != nil {
		return nil, err
	}
	var result []*PaymentRecord
	search := newSearch(own, 1)
	search.AddWhereN("ID", id)
	if err := getDataAction().Load(search, &result); err != nil || len(result) == 0 {
		return nil, err
	}
	return result[0], nil
}

// QueryByOrder 查询订单的全部支付尝试。
func (own *PaymentRecord) QueryByOrder(orderID uint) ([]*PaymentRecord, error) {
	if err := ensureModel(own); err != nil {
		return nil, err
	}
	var result []*PaymentRecord
	search := newSearch(own, 500)
	search.AddWhereN("OrderID", orderID)
	search.AddSortN("Attempt", false)
	err := getDataAction().Load(search, &result)
	return result, err
}

// ExistsByPaymentTypeID 判断支付类型是否已被历史流水引用。
func (own *PaymentRecord) ExistsByPaymentTypeID(paymentTypeID uint) (bool, error) {
	if err := ensureModel(own); err != nil {
		return false, err
	}
	var result []*PaymentRecord
	search := newSearch(own, 1)
	search.AddWhereN("PaymentTypeID", paymentTypeID)
	if err := getDataAction().Load(search, &result); err != nil {
		return false, err
	}
	return len(result) > 0, nil
}

// NextAttempt 返回订单下一次支付尝试序号。
func (own *PaymentRecord) NextAttempt(orderID uint) (int, error) {
	items, err := own.QueryByOrder(orderID)
	if err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 1, nil
	}
	return items[len(items)-1].Attempt + 1, nil
}
