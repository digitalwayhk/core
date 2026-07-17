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

func FindByIdempotency(key string) (*Order, error) {
	return findOrderWith(store.Get(), "IdempotencyKey", strings.TrimSpace(key))
}

func FindByIdempotencyWith(action persistencetypes.IDataAction, key string) (*Order, error) {
	return findOrderWith(action, "IdempotencyKey", strings.TrimSpace(key))
}

func FindOrder(id uint) (*Order, error) {
	return findOrderWith(store.Get(), "ID", id)
}

func FindOrderWith(action persistencetypes.IDataAction, id uint) (*Order, error) {
	return findOrderWith(action, "ID", id)
}

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

func (o *Order) InsertWith(action persistencetypes.IDataAction) error {
	if strings.TrimSpace(o.IdempotencyKey) == "" || strings.TrimSpace(o.RequestFingerprint) == "" || o.UserID == 0 || o.SupplierID == 0 || o.ProductID == 0 || o.Quantity <= 0 || o.OrderRevision == 0 {
		return errors.New("订单参数不完整")
	}
	o.SetHashcode(o.GetHash())
	return action.Insert(o)
}

func (o *Order) UpdateWith(action persistencetypes.IDataAction) error {
	o.SetUpdatedAt(time.Now().UTC())
	return action.Update(o)
}
