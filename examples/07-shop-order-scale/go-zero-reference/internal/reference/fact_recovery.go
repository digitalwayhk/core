// 本文件只在接受请求前核对历史不可达事实；无远端证明不删除，也不周期扫描重算计数。
package reference

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dgraph-io/badger/v4"
)

func (s *pending) reconcileFactOrphans(verify func([]*Order) error) error {
	var missing []pendingFactOrphan
	err := s.db.View(func(tx *badger.Txn) error {
		it := tx.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		for it.Seek([]byte("p:")); it.ValidForPrefix([]byte("p:")); it.Next() {
			key := it.Item().KeyCopy(nil)
			data, e := it.Item().ValueCopy(nil)
			if e != nil {
				return e
			}
			var order Order
			if e = json.Unmarshal(data, &order); e != nil {
				return e
			}
			if order.ID == 0 || order.UserID == 0 || order.RequestID == "" || !bytes.Equal(key, localKey(&order)) {
				return errors.New("本地事实身份损坏，保留现场")
			}
			q, e := tx.Get(queueKey(&order))
			if errors.Is(e, badger.ErrKeyNotFound) {
				missing = append(missing, pendingFactOrphan{key: key, value: data, order: &order})
				continue
			}
			if e != nil {
				return e
			}
			value, e := q.ValueCopy(nil)
			if e != nil {
				return e
			}
			if !bytes.Equal(value, key) {
				return errors.New("本地索引身份不匹配，保留现场")
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(missing) == 0 {
		return nil
	}
	if verify == nil {
		return errors.New("不可达事实缺少远端证明入口")
	}
	orders := make([]*Order, 0, len(missing))
	for _, o := range missing {
		orders = append(orders, o.order)
	}
	if err := verify(orders); err != nil {
		return err
	}
	for start := 0; start < len(missing); start += 512 {
		s.mu.Lock()
		removed := int64(0)
		err = s.db.Update(func(tx *badger.Txn) error {
			for _, o := range missing[start:min(start+512, len(missing))] {
				value, e := tx.Get(o.key)
				if errors.Is(e, badger.ErrKeyNotFound) {
					continue
				}
				if e != nil {
					return e
				}
				current, e := value.ValueCopy(nil)
				if e != nil {
					return e
				}
				if !bytes.Equal(current, o.value) {
					return errors.New("恢复期间本地事实版本变化，保留现场")
				}
				if _, e := tx.Get(queueKey(o.order)); e == nil {
					return errors.New("恢复期间本地索引变化，保留现场")
				} else if !errors.Is(e, badger.ErrKeyNotFound) {
					return e
				}
				if e := tx.Delete(o.key); e != nil {
					return e
				}
				removed++
			}
			return nil
		})
		if err == nil {
			s.depth.Add(-removed)
		}
		s.mu.Unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *application) verifyFactOrphans(items []*Order) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for start := 0; start < len(items); start += 512 {
		batch := items[start:min(start+512, len(items))]
		ids := make([]uint, 0, len(batch))
		events := make([]string, 0, len(batch))
		for _, o := range batch {
			ids = append(ids, o.ID)
			events = append(events, fmt.Sprintf("order:%d:OrderCreated", o.ID))
		}
		var orders []*Order
		var outboxes []*OutboxRecord
		if err := a.db.WithContext(ctx).Where("id IN ?", ids).Find(&orders).Error; err != nil {
			return err
		}
		if err := a.db.WithContext(ctx).Where("event_id IN ?", events).Find(&outboxes).Error; err != nil {
			return err
		}
		byID := map[uint]*Order{}
		byEvent := map[string]*OutboxRecord{}
		for _, o := range orders {
			byID[o.ID] = o
		}
		for _, o := range outboxes {
			byEvent[o.EventID] = o
		}
		for _, local := range batch {
			remote := byID[local.ID]
			outbox := byEvent[fmt.Sprintf("order:%d:OrderCreated", local.ID)]
			if remote == nil || outbox == nil || remote.UserID != local.UserID || remote.RequestID != local.RequestID || remote.RequestFingerprint != local.RequestFingerprint ||
				remote.SupplierID != local.SupplierID || remote.ProductID != local.ProductID || remote.Quantity != local.Quantity || !remote.UnitPrice.Equal(local.UnitPrice) || !remote.TotalAmount.Equal(local.TotalAmount) ||
				remote.SupplierCode != local.SupplierCode || remote.SupplierName != local.SupplierName || remote.ProductCode != local.ProductCode || remote.ProductName != local.ProductName ||
				remote.Recipient != local.Recipient || remote.Phone != local.Phone || remote.Region != local.Region || remote.AddressDetail != local.AddressDetail || remote.AddressID != local.AddressID {
				return errors.New("不可达事实缺少匹配的远端订单与Outbox证明，保留现场")
			}
			var event orderChanged
			if err := json.Unmarshal(outbox.Payload, &event); err != nil {
				return err
			}
			if event.OrderID != local.ID || event.UserID != local.UserID || event.EventID != outbox.EventID || event.SupplierID != local.SupplierID || event.ProductID != local.ProductID || outbox.EventType != "OrderCreated" {
				return errors.New("不可达事实的Outbox身份不匹配，保留现场")
			}
		}
	}
	return nil
}
