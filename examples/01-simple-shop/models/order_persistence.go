package models

import (
	"strings"
	"time"

	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// Insert 规范化秒级创建时间和唯一哈希后，直接通过模型数据适配器写入订单。
func (own *Order) Insert() error {
	createdAt := time.Now().UTC().Truncate(time.Second)
	if own.CreatedAt != nil {
		createdAt = own.CreatedAt.UTC().Truncate(time.Second)
	}
	own.SetCreatedAt(createdAt)
	own.SetUpdatedAt(createdAt)
	own.SetHashcode(own.GetHash())
	err := getDataAction().Insert(own)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return NewBusinessError("同一用户每秒只能购买一次同一商品")
	}
	return err
}

// QueryByUser 直接通过模型数据适配器查询指定用户的全部订单。
func (own *Order) QueryByUser(userID string) ([]*Order, error) {
	search := newOrderSearch(own, 500)
	search.AddWhereN("UserID", strings.TrimSpace(userID))
	var orders []*Order
	err := getDataAction().Load(search, &orders)
	return orders, err
}

// FindOwned 使用订单 ID 与用户 ID 组合条件查找当前用户的订单。
func (own *Order) FindOwned(id uint, userID string) (*Order, error) {
	search := newOrderSearch(own, 1)
	search.AddWhereN("ID", id)
	search.AddWhereN("UserID", strings.TrimSpace(userID))
	var orders []*Order
	if err := getDataAction().Load(search, &orders); err != nil {
		return nil, err
	}
	if len(orders) == 0 {
		return nil, nil
	}
	return orders[0], nil
}

// Delete 直接通过模型数据适配器物理删除已查询的订单。
func (own *Order) Delete() error {
	return getDataAction().Delete(own)
}

// newOrderSearch 创建模型直接查询所需的统一 SearchItem。
func newOrderSearch(model *Order, size int) *persistencetypes.SearchItem {
	return &persistencetypes.SearchItem{Page: 1, Size: size, Model: model}
}
