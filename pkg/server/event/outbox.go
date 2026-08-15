package event

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/digitalwayhk/core/pkg/server/observability"
	"github.com/zeromicro/go-zero/core/logx"
)

// OutboxMessage 是框架 Outbox 发布器需要的最小事件记录。
// 业务模型负责把本地 Outbox 表转换成该结构，发布器不理解业务表。
type OutboxMessage struct {
	ID             uint
	EventID        string
	EventType      string
	Subject        string
	Payload        []byte
	TraceID        string
	IdempotencyKey string
	ShardKey       string
}

// OutboxStore 只负责访问本服务本地 Outbox 表。
// 可靠性来自业务事实与 Outbox 在同一数据库事务内提交。
//
// LoadPending 必须按持久化 earliest-first 顺序返回 unpublished 记录
// （通常按主键/创建时间升序）。跨重启的同 key 屏障依赖该顺序；乱序返回会导致越序发布。
type OutboxStore interface {
	LoadPending(ctx context.Context, limit int) ([]OutboxMessage, error)
	MarkPublished(ctx context.Context, message OutboxMessage) error
}

// OutboxStoreSkipBlocked 是可选扩展：LoadPending 时跳过指定 OrderingKey（ShardKey），
// 避免单 hot key 卡死占满 batch 后饿死其他 key。实现方应对 skip 后的结果仍保持 earliest-first。
type OutboxStoreSkipBlocked interface {
	LoadPendingSkipping(ctx context.Context, limit int, skipOrderingKeys []string) ([]OutboxMessage, error)
}

type OutboxOptions struct {
	SourceService string
	Store         OutboxStore
	Interval      time.Duration
	BatchSize     int
	External      bool
}

type outboxPublisher struct {
	source     string
	store      OutboxStore
	interval   time.Duration
	batch      int
	external   bool
	bridge     *ServiceEventBridge
	notify     chan struct{}
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	depth      atomic.Int64
	failures   atomic.Uint64
	loadFailed atomic.Bool
}

func newOutboxPublisher(bridge *ServiceEventBridge, options OutboxOptions) (*outboxPublisher, error) {
	if bridge == nil || bridge.closed.Load() {
		return nil, ErrServiceEventBridgeClosed
	}
	if options.Store == nil {
		return nil, errors.New("event outbox store is nil")
	}
	if options.SourceService == "" {
		return nil, errors.New("event outbox source service is empty")
	}
	if options.Interval <= 0 {
		options.Interval = 100 * time.Millisecond
	}
	if options.BatchSize <= 0 {
		options.BatchSize = 100
	}
	ctx, cancel := context.WithCancel(bridge.ctx)
	publisher := &outboxPublisher{
		source: options.SourceService, store: options.Store, interval: options.Interval,
		batch: options.BatchSize, external: options.External, bridge: bridge,
		notify: make(chan struct{}, 1), cancel: cancel,
	}
	publisher.wg.Add(1)
	go publisher.run(ctx)
	return publisher, nil
}

func (p *outboxPublisher) run(ctx context.Context) {
	defer p.wg.Done()
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.notify:
			p.drain(ctx)
		case <-ticker.C:
			p.drain(ctx)
		}
	}
}

func (p *outboxPublisher) drain(ctx context.Context) {
	// 本轮同 OrderingKey 失败屏障：最早失败的 key 阻断后续同 key 记录，其他 key 可继续。
	// 跨重启依赖 LoadPending 仍按 earliest-first 返回 unpublished。
	// 若 store 实现 OutboxStoreSkipBlocked，可跳过已 blocked 的 key，避免 hot key 饿死其他 key。
	blocked := make(map[string]struct{})
	noProgressRounds := 0
	for {
		items, err := p.loadPending(ctx, blocked)
		if err != nil {
			p.loadFailed.Store(true)
			p.failures.Add(1)
			logx.Errorw("event_outbox_load_failed", logx.Field("service", p.source), logx.Field("error", err))
			return
		}
		p.loadFailed.Store(false)
		p.depth.Store(int64(len(items)))
		if len(items) == 0 {
			return
		}
		progressed := false
		for _, item := range items {
			key := outboxOrderingKey(item)
			if _, skip := blocked[key]; skip {
				continue
			}
			if err := p.publish(ctx, item); err != nil {
				p.failures.Add(1)
				logx.Errorw("event_outbox_publish_failed", logx.Field("service", p.source), logx.Field("event_type", item.EventType), logx.Field("event_id", item.EventID), logx.Field("ordering_key", key), logx.Field("error", err))
				blocked[key] = struct{}{}
				continue
			}
			if err := p.store.MarkPublished(ctx, item); err != nil {
				p.failures.Add(1)
				logx.Errorw("event_outbox_mark_failed", logx.Field("service", p.source), logx.Field("event_type", item.EventType), logx.Field("event_id", item.EventID), logx.Field("error", err))
				// Mark 失败允许同 EventID 重发；本轮仍阻断同 key，避免后续记录抢先发布。
				blocked[key] = struct{}{}
				continue
			}
			p.depth.Add(-1)
			progressed = true
		}
		if progressed {
			noProgressRounds = 0
			if len(items) < p.batch {
				return
			}
			continue
		}
		// 本批无进展：若 store 支持 skip blocked key，再拉一轮，避免 hot key 饿死其他 key。
		noProgressRounds++
		if noProgressRounds >= 2 {
			return
		}
		if _, ok := p.store.(OutboxStoreSkipBlocked); ok && len(blocked) > 0 {
			continue
		}
		return
	}
}

func (p *outboxPublisher) loadPending(ctx context.Context, blocked map[string]struct{}) ([]OutboxMessage, error) {
	if len(blocked) > 0 {
		if skipper, ok := p.store.(OutboxStoreSkipBlocked); ok {
			keys := make([]string, 0, len(blocked))
			for k := range blocked {
				keys = append(keys, k)
			}
			return skipper.LoadPendingSkipping(ctx, p.batch, keys)
		}
	}
	return p.store.LoadPending(ctx, p.batch)
}

func outboxOrderingKey(item OutboxMessage) string {
	if item.ShardKey != "" {
		return item.ShardKey
	}
	if item.EventID != "" {
		return item.EventType + ":" + item.EventID
	}
	return item.EventType + ":unknown"
}

func (p *outboxPublisher) publish(ctx context.Context, item OutboxMessage) error {
	env := NewEnvelope(p.source, item.EventType, item.Payload)
	if item.EventID != "" {
		env.ID = item.EventID
	}
	env.Subject = item.Subject
	env.TraceID = item.TraceID
	env.IdempotencyKey = item.IdempotencyKey
	if env.IdempotencyKey == "" {
		env.IdempotencyKey = env.ID
	}
	env.ShardKey = item.ShardKey
	if env.ShardKey == "" {
		if p.bridge != nil && p.bridge.RequiresOrderedReliable() {
			return ErrOrderingKeyRequired
		}
		env.ShardKey = item.EventType + ":" + env.ID
	}
	err := p.bridge.Publish(ctx, PublishRequest{Class: ControlDelivery, External: p.external, Subject: item.Subject, Envelope: env})
	// Outbox 是示例 07 的真实发布路径；必须在此记录发布指标，才能拼出异步边。
	result := observability.ResultSuccess
	if err != nil {
		result = observability.ClassifyError(err)
	}
	subject := item.Subject
	if subject == "" && env != nil {
		subject = env.Subject
	}
	observability.RecordEventPublish(p.source, subject, item.EventType, result)
	return err
}

func (p *outboxPublisher) notifyNow() {
	if p == nil {
		return
	}
	select {
	case p.notify <- struct{}{}:
	default:
	}
}

func (p *outboxPublisher) close() {
	if p == nil {
		return
	}
	p.cancel()
	p.wg.Wait()
}
