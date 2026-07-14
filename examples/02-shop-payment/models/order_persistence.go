package models

import (
	"strings"
	"time"
)

// Insert 规范化秒级创建时间并写入订单。
func (own *Order) Insert() error {
	createdAt := time.Now().UTC().Truncate(time.Second)
	if own.CreatedAt != nil {
		createdAt = own.CreatedAt.UTC().Truncate(time.Second)
	}
	own.SetCreatedAt(createdAt)
	own.SetUpdatedAt(createdAt)
	own.SetHashcode(own.GetHash())
	if err := getDataAction().Insert(own); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return NewBusinessError("同一用户每秒只能购买一次同一商品")
		}
		return err
	}
	return nil
}

// Update 保存订单状态变化。
func (own *Order) Update() error {
	own.SetUpdatedAt(time.Now().UTC())
	return getDataAction().Update(own)
}

// Delete 物理删除订单。
func (own *Order) Delete() error { return getDataAction().Delete(own) }

// FindByID 按 ID 查询订单。
func (own *Order) FindByID(id uint) (*Order, error) {
	if err := ensureModel(own); err != nil {
		return nil, err
	}
	var result []*Order
	search := newSearch(own, 1)
	search.AddWhereN("ID", id)
	if err := getDataAction().Load(search, &result); err != nil || len(result) == 0 {
		return nil, err
	}
	return result[0], nil
}

// FindOwned 按 ID 和可信用户查找订单。
func (own *Order) FindOwned(id uint, userID string) (*Order, error) {
	if err := ensureModel(own); err != nil {
		return nil, err
	}
	var result []*Order
	search := newSearch(own, 1)
	search.AddWhereN("ID", id)
	search.AddWhereN("UserID", strings.TrimSpace(userID))
	if err := getDataAction().Load(search, &result); err != nil || len(result) == 0 {
		return nil, err
	}
	return result[0], nil
}

// QueryByUser 查询指定用户的订单。
func (own *Order) QueryByUser(userID string) ([]*Order, error) {
	if err := ensureModel(own); err != nil {
		return nil, err
	}
	var result []*Order
	search := newSearch(own, 500)
	search.AddWhereN("UserID", strings.TrimSpace(userID))
	search.AddSortN("ID", false)
	err := getDataAction().Load(search, &result)
	return result, err
}

// ExistsByProductID 判断商品是否已被历史订单引用。
func (own *Order) ExistsByProductID(productID uint) (bool, error) {
	if err := ensureModel(own); err != nil {
		return false, err
	}
	var result []*Order
	search := newSearch(own, 1)
	search.AddWhereN("ProductID", productID)
	if err := getDataAction().Load(search, &result); err != nil {
		return false, err
	}
	return len(result) > 0, nil
}
