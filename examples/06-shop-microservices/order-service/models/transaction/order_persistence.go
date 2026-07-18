// 本文件定义当前服务交易事实、Outbox、Inbox 或投影模型能力。
package transaction

import (
	"errors"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/internal/store"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

func findOrderWith(action persistencetypes.IDataAction, field string, value interface{}) (*Order, error) {
	store.Lock()
	defer store.Unlock()
	var ensureItems []*Order
	if err := action.Load(store.NewSearch(NewOrder(), 1), &ensureItems); err != nil {
		return nil, err
	}
	var items []*Order
	query := store.NewSearch(NewOrder(), 1)
	query.AddWhereN(field, value)
	if err := action.Load(query, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

// FindByIdempotency 执行本文件能力对应的业务操作。
func FindByIdempotency(key string) (*Order, error) {
	return findOrderWith(store.Get(), "IdempotencyKey", strings.TrimSpace(key))
}

// FindByIdempotencyWith 执行本文件能力对应的业务操作。
func FindByIdempotencyWith(action persistencetypes.IDataAction, key string) (*Order, error) {
	return findOrderWith(action, "IdempotencyKey", strings.TrimSpace(key))
}

// FindOrder 执行本文件能力对应的业务操作。
func FindOrder(id uint) (*Order, error) {
	return findOrderWith(store.Get(), "ID", id)
}

// FindOrderWith 执行本文件能力对应的业务操作。
func FindOrderWith(action persistencetypes.IDataAction, id uint) (*Order, error) {
	return findOrderWith(action, "ID", id)
}

// ListOrders 执行本文件能力对应的业务操作。
func ListOrders(field string, value interface{}) ([]*Order, error) {
	if err := store.EnsureModel(NewOrder()); err != nil {
		return nil, err
	}
	var items []*Order
	query := store.NewSearch(NewOrder(), 1000)
	query.AddWhereN(field, value)
	query.AddSortN("ID", false)
	err := store.Get().Load(query, &items)
	return items, err
}

// InsertWith 实现本类型在当前服务边界中的行为。
func (o *Order) InsertWith(action persistencetypes.IDataAction) error {
	if strings.TrimSpace(o.IdempotencyKey) == "" || strings.TrimSpace(o.RequestFingerprint) == "" || o.UserID == 0 || o.SupplierID == 0 || o.ProductID == 0 || o.Quantity <= 0 || o.OrderRevision == 0 {
		return errors.New("订单参数不完整")
	}
	o.SetHashcode(o.GetHash())
	return action.Insert(o)
}

// UpdateWith 实现本类型在当前服务边界中的行为。
func (o *Order) UpdateWith(action persistencetypes.IDataAction) error {
	o.SetUpdatedAt(time.Now().UTC())
	return action.Update(o)
}
