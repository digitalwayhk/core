// Package runtime 提供示例 06 三个服务共用的无业务模型运行时。
package runtime

import (
	"context"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/zeromicro/go-zero/core/logx"
)

type LoadOutbox func() ([]OutboxRecord, error)
type MarkPublished func(OutboxRecord) error

// OutboxWorker 只在 EventBridge 外发成功后标记记录，失败保留等待下轮重试。
type OutboxWorker struct {
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func StartOutboxWorker(parent context.Context, service string, bridge *event.ServiceEventBridge, load LoadOutbox, mark MarkPublished) *OutboxWorker {
	ctx, cancel := context.WithCancel(parent)
	worker := &OutboxWorker{cancel: cancel}
	worker.wg.Add(1)
	go func() {
		defer worker.wg.Done()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				records, err := load()
				if err != nil {
					logx.Errorw("event_outbox_load_failed", logx.Field("service", service), logx.Field("error", err))
					continue
				}
				for _, record := range records {
					env := event.NewEnvelope(service, record.EventType, record.Payload)
					env.ID = record.EventID
					env.Subject = record.Subject
					env.IdempotencyKey = record.EventID
					env.ShardKey = record.EventType + ":" + record.EventID
					if err := bridge.Publish(ctx, event.PublishRequest{Class: event.ControlDelivery, External: true, Subject: record.Subject, Envelope: env}); err != nil {
						logx.Errorw("event_outbox_publish_failed", logx.Field("service", service), logx.Field("event_type", record.EventType), logx.Field("event_id", record.EventID), logx.Field("error", err))
						continue
					}
					if err := mark(record); err != nil {
						logx.Errorw("event_outbox_mark_failed", logx.Field("service", service), logx.Field("event_type", record.EventType), logx.Field("event_id", record.EventID), logx.Field("error", err))
					}
				}
			}
		}
	}()
	return worker
}

func (w *OutboxWorker) Stop() {
	if w == nil {
		return
	}
	w.cancel()
	w.wg.Wait()
}
