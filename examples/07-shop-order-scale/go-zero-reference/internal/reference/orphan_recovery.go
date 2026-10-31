// 本文件仅在远端订单与 Outbox 均能证明原事实后，允许清理无数据的派生索引。
package reference

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"
)

func (a *application) verifyOrphans(items []queueOrphan) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for offset := 0; offset < len(items); offset += 512 {
		batch := items[offset:min(offset+512, len(items))]
		ids := make([]uint, 0, len(batch))
		events := make([]string, 0, len(batch))
		for _, o := range batch {
			ids = append(ids, o.id)
			events = append(events, fmt.Sprintf("order:%d:OrderCreated", o.id))
		}
		var orders []Order
		var outboxes []OutboxRecord
		if e := a.db.WithContext(ctx).Where("id IN ?", ids).Find(&orders).Error; e != nil {
			return e
		}
		if e := a.db.WithContext(ctx).Select("id", "event_id").Where("event_id IN ?", events).Find(&outboxes).Error; e != nil {
			return e
		}
		byID := map[uint]*Order{}
		for i := range orders {
			byID[orders[i].ID] = &orders[i]
		}
		byEvent := map[string]bool{}
		for _, o := range outboxes {
			byEvent[o.EventID] = true
		}
		for _, o := range batch {
			order := byID[o.id]
			if order == nil || !bytes.Equal(localKey(order), o.dataKey) || !byEvent[fmt.Sprintf("order:%d:OrderCreated", o.id)] {
				return errors.New("孤儿索引缺少匹配的远端订单和 Outbox 证明，保留现场")
			}
		}
	}
	return nil
}
