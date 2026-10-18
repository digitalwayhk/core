// 本文件直接用 GORM 与 go-redis 实现 Outbox 发布、成功确认、消费去重及 PEL 回收。
package reference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const streamName = "core:event:07:shop.order.changed"

func (a *application) newOutbox(o *Order) (*OutboxRecord, error) {
	eventID := fmt.Sprintf("order:%d:OrderCreated", o.ID)
	id, e := a.clock.next()
	if e != nil {
		return nil, e
	}
	now := time.Now().UTC()
	payload := orderChanged{SchemaVersion: 1, EventID: eventID, EventType: "OrderCreated", Subject: "shop.order.changed", TraceID: o.TraceID, ServiceName: o.ServiceName, ServiceInstanceID: o.ServiceInstanceID, OccurredAt: *o.CreatedAt, OrderID: o.ID, OrderRevision: o.OrderRevision, UserID: o.UserID, SupplierID: o.SupplierID, ProductID: o.ProductID, OrderStatus: o.OrderStatus, PaymentStatus: o.PaymentStatus}
	b, e := json.Marshal(payload)
	if e != nil {
		return nil, e
	}
	return &OutboxRecord{Record: Record{ID: id, CreatedAt: &now, UpdatedAt: &now, Hashcode: hashCodes(eventID), TraceID: o.TraceID, ServiceName: o.ServiceName, ServiceInstanceID: o.ServiceInstanceID, ServiceInstanceIP: o.ServiceInstanceIP}, EventID: eventID, EventType: "OrderCreated", Subject: "shop.order.changed", Payload: b}, nil
}

func (a *application) publishLoop() {
	for a.ctx.Err() == nil {
		select {
		case <-a.ctx.Done():
			return
		case <-a.notify:
		case <-time.After(100 * time.Millisecond):
		}
		for a.ctx.Err() == nil {
			var pending []*OutboxRecord
			if e := a.db.WithContext(a.ctx).Where("published = ?", false).Order("id ASC").Limit(100).Find(&pending).Error; e != nil {
				recordError("outbox_load", e)
				break
			}
			if len(pending) == 0 {
				break
			}
			published, e := a.publishBatch(a.ctx, pending)
			if len(published) > 0 {
				if markErr := a.markPublished(a.ctx, published); markErr != nil {
					recordError("outbox_mark", markErr)
					break
				}
			}
			if e != nil {
				recordError("outbox_publish", e)
				break
			}
			if len(published) == 0 {
				break
			}
		}
	}
}

func (a *application) publishBatch(ctx context.Context, items []*OutboxRecord) ([]*OutboxRecord, error) {
	lanes := [4][]*OutboxRecord{}
	for _, o := range items {
		var payload orderChanged
		if e := json.Unmarshal(o.Payload, &payload); e != nil {
			return nil, e
		}
		if payload.OrderID == 0 {
			return nil, errors.New("事件缺少订单 ID")
		}
		n := orderLane(payload.OrderID, 4)
		lanes[n] = append(lanes[n], o)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstError error
	var published []*OutboxRecord
	for _, lane := range lanes {
		wg.Go(func() {
			blocked := map[string]bool{}
			for _, o := range lane {
				var payload orderChanged
				if e := json.Unmarshal(o.Payload, &payload); e != nil {
					mu.Lock()
					firstError = e
					mu.Unlock()
					return
				}
				key := fmt.Sprintf("shop-order:order:%d", payload.OrderID)
				if blocked[key] {
					continue
				}
				env := envelope{ID: o.EventID, Source: "shop-order", SpecVersion: "1.0", Type: o.EventType, Subject: o.Subject, Time: time.Now().UTC(), DataContentType: "application/json", Data: o.Payload, TraceID: o.TraceID, IdempotencyKey: o.EventID, ShardKey: key}
				b, e := json.Marshal(env)
				if e == nil {
					e = a.redis.XAdd(ctx, &redis.XAddArgs{Stream: streamName, Values: map[string]interface{}{"data": b, "idempotency_key": o.EventID, "ordering_key": key}}).Err()
				}
				mu.Lock()
				if e != nil {
					firstError = e
					blocked[key] = true
				} else {
					published = append(published, o)
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return published, firstError
}

func isBusyGroup(e error) bool { return e != nil && strings.Contains(e.Error(), "BUSYGROUP") }

func (a *application) handleMessages(messages []redis.XMessage) {
	lanes := [8][]redis.XMessage{}
	for _, m := range messages {
		var env envelope
		raw, ok := m.Values["data"].(string)
		if !ok || json.Unmarshal([]byte(raw), &env) != nil {
			continue
		}
		var payload orderChanged
		if json.Unmarshal(env.Data, &payload) != nil || payload.SchemaVersion != 1 || payload.EventID == "" || payload.OrderID == 0 || payload.UserID == 0 {
			continue
		}
		n := orderLane(payload.OrderID, 8)
		lanes[n] = append(lanes[n], m)
	}
	var wg sync.WaitGroup
	for _, lane := range lanes {
		wg.Go(func() {
			for _, m := range lane {
				var env envelope
				if e := json.Unmarshal([]byte(m.Values["data"].(string)), &env); e != nil {
					break
				}
				var payload orderChanged
				if e := json.Unmarshal(env.Data, &payload); e != nil {
					break
				}
				// 07 user 是无持久用户库的 facade；同样只做进程内幂等，不能冒充持久 Inbox。
				a.processed.LoadOrStore(payload.EventType+":"+payload.EventID, struct{}{})
				if e := a.redis.XAck(a.ctx, streamName, "shop-user", m.ID).Err(); e != nil {
					recordError("consume_ack", e)
					break
				}
			}
		})
	}
	wg.Wait()
}

func (a *application) consume() {
	consumer := "native-user"
	lastClaim := time.Time{}
	for a.ctx.Err() == nil {
		if time.Since(lastClaim) >= 15*time.Second {
			cursor := "0-0"
			for {
				messages, next, e := a.redis.XAutoClaim(a.ctx, &redis.XAutoClaimArgs{Stream: streamName, Group: "shop-user", Consumer: consumer, MinIdle: 30 * time.Second, Start: cursor, Count: 32}).Result()
				if e != nil {
					if !errors.Is(e, redis.Nil) {
						recordError("consume_reclaim", e)
					}
					break
				}
				a.handleMessages(messages)
				if next == "0-0" {
					break
				}
				cursor = next
			}
			lastClaim = time.Now()
		}
		entries, e := a.redis.XReadGroup(a.ctx, &redis.XReadGroupArgs{Group: "shop-user", Consumer: consumer, Streams: []string{streamName, ">"}, Count: 32, Block: time.Second}).Result()
		if e != nil {
			if !errors.Is(e, redis.Nil) {
				recordError("consume_read", e)
				if !wait(a.ctx, 100*time.Millisecond) {
					return
				}
			}
			continue
		}
		for _, entry := range entries {
			a.handleMessages(entry.Messages)
		}
	}
}
