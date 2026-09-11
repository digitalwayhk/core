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

// OutboxBatchMarker 是可选扩展：一次事务确认一批已成功发布到 MQ 的记录。
// 未实现时框架继续逐条调用 MarkPublished。空切片必须立即成功且不开事务。
// 实现必须单事务全成或全败；已确认记录视为成功。崩溃发生在 MQ 发布后、
// 本方法提交前时只形成 at-least-once 重复投递，消费者继续用 EventID 幂等。
// nil 仅表示全部请求记录已持久确认；缺失/无效记录必须返回错误，不得静默跳过。
// 确认失败可能重放整批已发布前缀；同 key 串行 Publish 不代表重复消息也单调有序。
// 此确认只更新 Outbox 发布状态，不代表消费者 ACK，也不授权回收 Broker 消息。
type OutboxBatchMarker interface {
	MarkPublishedBatch(ctx context.Context, messages []OutboxMessage) error
}

type OutboxOptions struct {
	SourceService string
	Store         OutboxStore
	Interval      time.Duration
	BatchSize     int
	// KeyConcurrency 是同一次 drain 中可并行推进的 OrderingKey 数。
	// 零值和 1 保持历史串行行为；只有调用方显式设置大于 1 才启用分键并发。
	KeyConcurrency int
	External       bool
}

type outboxPublisher struct {
	source         string
	store          OutboxStore
	interval       time.Duration
	batch          int
	keyConcurrency int
	external       bool
	bridge         *ServiceEventBridge
	notify         chan struct{}
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	depth          atomic.Int64
	failures       atomic.Uint64
	loadFailed     atomic.Bool
	lastBatch      atomic.Int64
	activeLanes    atomic.Int64
	blockedKeys    atomic.Int64
	workerInflight atomic.Int64
	workerPeak     atomic.Int64
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
	if options.KeyConcurrency <= 0 {
		options.KeyConcurrency = 1
	}
	ctx, cancel := context.WithCancel(bridge.ctx)
	publisher := &outboxPublisher{
		source: options.SourceService, store: options.Store, interval: options.Interval,
		batch: options.BatchSize, keyConcurrency: options.KeyConcurrency,
		external: options.External, bridge: bridge,
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
	p.blockedKeys.Store(0)
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
		p.lastBatch.Store(int64(len(items)))
		if len(items) == 0 {
			return
		}
		progressed := p.publishBatch(ctx, items, blocked)
		p.blockedKeys.Store(int64(len(blocked)))
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

type outboxLaneResult struct {
	key       string
	published []OutboxMessage
	failed    bool
}

// publishBatch 在同 key 内逐条 Publish，在不同 key 间按显式上限并行。
// 若 store 实现 OutboxBatchMarker，先发布成功前缀再一次确认；否则保持逐条 MarkPublished。
// blocked 只由调用方 goroutine 更新，worker 不共享修改该 map。
func (p *outboxPublisher) publishBatch(
	ctx context.Context,
	items []OutboxMessage,
	blocked map[string]struct{},
) bool {
	marker, batchMark := p.store.(OutboxBatchMarker)
	if p.keyConcurrency <= 1 {
		if batchMark {
			return p.publishSerialThenMarkBatch(ctx, items, blocked, marker)
		}
		return p.publishSerialAndMarkEach(ctx, items, blocked)
	}
	if batchMark {
		return p.publishKeyedThenMarkBatch(ctx, items, blocked, marker)
	}
	return p.publishKeyedAndMarkEach(ctx, items, blocked)
}

func (p *outboxPublisher) publishSerialAndMarkEach(
	ctx context.Context,
	items []OutboxMessage,
	blocked map[string]struct{},
) bool {
	progressed := false
	p.activeLanes.Store(1)
	defer p.activeLanes.Store(0)
	for _, item := range items {
		key := outboxOrderingKey(item)
		if _, skip := blocked[key]; skip {
			continue
		}
		p.beginOutboxWorker()
		err := p.publish(ctx, item)
		if err != nil {
			p.endOutboxWorker()
			p.failures.Add(1)
			logx.Errorw("event_outbox_publish_failed", logx.Field("service", p.source), logx.Field("event_type", item.EventType), logx.Field("event_id", item.EventID), logx.Field("ordering_key", key), logx.Field("error", err))
			blocked[key] = struct{}{}
			continue
		}
		if err := p.store.MarkPublished(ctx, item); err != nil {
			p.endOutboxWorker()
			p.failures.Add(1)
			logx.Errorw("event_outbox_mark_failed", logx.Field("service", p.source), logx.Field("event_type", item.EventType), logx.Field("event_id", item.EventID), logx.Field("ordering_key", key), logx.Field("error", err))
			blocked[key] = struct{}{}
			continue
		}
		p.endOutboxWorker()
		p.depth.Add(-1)
		progressed = true
	}
	return progressed
}

func (p *outboxPublisher) publishSerialThenMarkBatch(
	ctx context.Context,
	items []OutboxMessage,
	blocked map[string]struct{},
	marker OutboxBatchMarker,
) bool {
	published := make([]OutboxMessage, 0, len(items))
	p.activeLanes.Store(1)
	defer p.activeLanes.Store(0)
	for _, item := range items {
		key := outboxOrderingKey(item)
		if _, skip := blocked[key]; skip {
			continue
		}
		p.beginOutboxWorker()
		err := p.publish(ctx, item)
		p.endOutboxWorker()
		if err != nil {
			p.failures.Add(1)
			logx.Errorw("event_outbox_publish_failed", logx.Field("service", p.source), logx.Field("event_type", item.EventType), logx.Field("event_id", item.EventID), logx.Field("ordering_key", key), logx.Field("error", err))
			blocked[key] = struct{}{}
			continue
		}
		published = append(published, item)
	}
	return p.confirmPublishedBatch(ctx, published, blocked, marker)
}

func (p *outboxPublisher) splitUnblockedLanes(
	items []OutboxMessage,
	blocked map[string]struct{},
) (map[string][]OutboxMessage, []string) {
	lanes := make(map[string][]OutboxMessage)
	keys := make([]string, 0)
	for _, item := range items {
		key := outboxOrderingKey(item)
		if _, skip := blocked[key]; skip {
			continue
		}
		if _, exists := lanes[key]; !exists {
			keys = append(keys, key)
		}
		lanes[key] = append(lanes[key], item)
	}
	return lanes, keys
}

func (p *outboxPublisher) publishKeyedAndMarkEach(
	ctx context.Context,
	items []OutboxMessage,
	blocked map[string]struct{},
) bool {
	lanes, keys := p.splitUnblockedLanes(items, blocked)
	if len(keys) == 0 {
		return false
	}
	results := p.runKeyedLanes(ctx, keys, lanes, true)
	progressed := false
	for _, result := range results {
		if len(result.published) > 0 {
			progressed = true
		}
		if result.failed {
			blocked[result.key] = struct{}{}
		}
	}
	return progressed
}

func (p *outboxPublisher) publishKeyedThenMarkBatch(
	ctx context.Context,
	items []OutboxMessage,
	blocked map[string]struct{},
	marker OutboxBatchMarker,
) bool {
	lanes, keys := p.splitUnblockedLanes(items, blocked)
	if len(keys) == 0 {
		return false
	}
	results := p.runKeyedLanes(ctx, keys, lanes, false)
	published := make([]OutboxMessage, 0)
	for _, result := range results {
		if result.failed {
			blocked[result.key] = struct{}{}
		}
		published = append(published, result.published...)
	}
	return p.confirmPublishedBatch(ctx, published, blocked, marker)
}

func (p *outboxPublisher) runKeyedLanes(
	ctx context.Context,
	keys []string,
	lanes map[string][]OutboxMessage,
	markEach bool,
) []outboxLaneResult {
	p.activeLanes.Store(int64(len(keys)))
	defer p.activeLanes.Store(0)

	workerLimit := p.keyConcurrency
	if workerLimit > len(keys) {
		workerLimit = len(keys)
	}
	results := make(chan outboxLaneResult, len(keys))
	semaphore := make(chan struct{}, workerLimit)
	var wg sync.WaitGroup
	for _, key := range keys {
		key := key
		lane := lanes[key]
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				results <- outboxLaneResult{key: key, failed: true}
				return
			}
			p.beginOutboxWorker()
			defer p.endOutboxWorker()
			result := outboxLaneResult{key: key}
			for _, item := range lane {
				if err := p.publish(ctx, item); err != nil {
					p.failures.Add(1)
					logx.Errorw("event_outbox_publish_failed", logx.Field("service", p.source), logx.Field("event_type", item.EventType), logx.Field("event_id", item.EventID), logx.Field("ordering_key", key), logx.Field("error", err))
					result.failed = true
					break
				}
				if markEach {
					if err := p.store.MarkPublished(ctx, item); err != nil {
						p.failures.Add(1)
						logx.Errorw("event_outbox_mark_failed", logx.Field("service", p.source), logx.Field("event_type", item.EventType), logx.Field("event_id", item.EventID), logx.Field("ordering_key", key), logx.Field("error", err))
						result.failed = true
						break
					}
					p.depth.Add(-1)
				}
				result.published = append(result.published, item)
			}
			results <- result
		}()
	}
	wg.Wait()
	close(results)
	out := make([]outboxLaneResult, 0, len(keys))
	for result := range results {
		out = append(out, result)
	}
	return out
}

func (p *outboxPublisher) confirmPublishedBatch(
	ctx context.Context,
	published []OutboxMessage,
	blocked map[string]struct{},
	marker OutboxBatchMarker,
) bool {
	if len(published) == 0 {
		return false
	}
	if err := marker.MarkPublishedBatch(ctx, published); err != nil {
		p.failures.Add(1)
		logx.Errorw("event_outbox_mark_failed", logx.Field("service", p.source), logx.Field("batch_size", len(published)), logx.Field("error", err))
		for _, item := range published {
			blocked[outboxOrderingKey(item)] = struct{}{}
		}
		return false
	}
	p.depth.Add(-int64(len(published)))
	return true
}

func (p *outboxPublisher) beginOutboxWorker() {
	current := p.workerInflight.Add(1)
	for {
		peak := p.workerPeak.Load()
		if current <= peak || p.workerPeak.CompareAndSwap(peak, current) {
			return
		}
	}
}

func (p *outboxPublisher) endOutboxWorker() {
	p.workerInflight.Add(-1)
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
