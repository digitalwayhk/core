package transaction

import (
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models/common"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/models/internal/store"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// Insert 规范化秒级创建时间并写入订单；Hashcode 由 ID 派生，调用方须先 SetID（如 req.NewID）。
func (own *Order) Insert() error {
	if own.GetID() == 0 {
		return common.NewValidationError("订单 ID 不能为空")
	}
	createdAt := time.Now().UTC().Truncate(time.Second)
	if own.CreatedAt != nil {
		createdAt = own.CreatedAt.UTC().Truncate(time.Second)
	}
	own.SetCreatedAt(createdAt)
	own.SetUpdatedAt(createdAt)
	own.SetHashcode(own.GetHash())
	return store.Get().Insert(own)
}

// Update 保存订单状态变化。
func (own *Order) Update() error {
	return own.UpdateWith(store.Get())
}

// UpdateWith 使用指定事务适配器保存订单状态变化。
func (own *Order) UpdateWith(action persistencetypes.IDataAction) error {
	own.SetUpdatedAt(time.Now().UTC())
	return action.Update(own)
}

// Delete 物理删除订单。
func (own *Order) Delete() error { return store.Get().Delete(own) }

// FindByID 按 ID 查询订单。
func (own *Order) FindByID(id uint) (*Order, error) {
	return own.FindByIDWith(store.Get(), id)
}

// FindByIDWith 使用指定事务适配器按 ID 查询订单。
func (own *Order) FindByIDWith(action persistencetypes.IDataAction, id uint) (*Order, error) {
	if err := store.EnsureModelWith(action, own); err != nil {
		return nil, err
	}
	var result []*Order
	search := store.NewSearch(own, 1)
	search.AddWhereN("ID", id)
	if err := action.Load(search, &result); err != nil || len(result) == 0 {
		return nil, err
	}
	return result[0], nil
}

// FindOwned 按 ID 和可信用户查找订单。
func (own *Order) FindOwned(id uint, userID string) (*Order, error) {
	return own.FindOwnedWith(store.Get(), id, userID)
}

// FindOwnedWith 使用指定事务适配器查找用户本人的订单。
func (own *Order) FindOwnedWith(action persistencetypes.IDataAction, id uint, userID string) (*Order, error) {
	if err := store.EnsureModelWith(action, own); err != nil {
		return nil, err
	}
	var result []*Order
	search := store.NewSearch(own, 1)
	search.AddWhereN("ID", id)
	search.AddWhereN("UserID", strings.TrimSpace(userID))
	if err := action.Load(search, &result); err != nil || len(result) == 0 {
		return nil, err
	}
	return result[0], nil
}

// QueryByUser 查询指定用户的订单。
func (own *Order) QueryByUser(userID string) ([]*Order, error) {
	if err := store.EnsureModel(own); err != nil {
		return nil, err
	}
	var result []*Order
	search := store.NewSearch(own, 500)
	search.AddWhereN("UserID", strings.TrimSpace(userID))
	search.AddSortN("ID", false)
	err := store.Get().Load(search, &result)
	return result, err
}

// ExistsByProductID 判断商品是否已被历史订单引用。
func (own *Order) ExistsByProductID(productID uint) (bool, error) {
	if err := store.EnsureModel(own); err != nil {
		return false, err
	}
	var result []*Order
	search := store.NewSearch(own, 1)
	search.AddWhereN("ProductID", productID)
	if err := store.Get().Load(search, &result); err != nil {
		return false, err
	}
	return len(result) > 0, nil
}
