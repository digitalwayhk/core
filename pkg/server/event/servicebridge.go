package event

import (
	"context"
	"errors"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrExternalProviderUnavailable = errors.New("event external provider unavailable")
	ErrServiceEventBridgeClosed    = errors.New("service event bridge closed")
	ErrInvalidPublishRequest       = errors.New("invalid event publish request")
	ErrControlQueueTimeout         = errors.New("service event bridge control queue timeout")
	ErrOrderedReliableUnsupported  = errors.New("event ordered reliable unsupported")
)

// orderedReliableEnsurer 由外部适配器（如 MQBridge）实现，用于启动 fail-closed 检查。
type orderedReliableEnsurer interface {
	EnsureOrderedReliable() error
	RequiresOrderedReliable() bool
}

const defaultControlEnqueueTimeout = 5 * time.Second

type DeliveryClass uint8

const (
	ObserverDelivery DeliveryClass = iota
	ControlDelivery
)

type PublishRequest struct {
	Class     DeliveryClass
	External  bool
	Subject   string
	Envelope  *Envelope
	BuildData func() ([]byte, error)
}

type ExternalPublisher interface {
	Publish(ctx context.Context, subject string, env *Envelope) error
}

type ExternalSubscriber interface {
	Subscribe(ctx context.Context, subject string) (func(), error)
}

// ReliableExternalSubscriber 只在控制事件需要成功后确认时使用。
// subscriberID 是稳定的逻辑服务名，同服务的多个实例共享一个消费组。
type ReliableExternalSubscriber interface {
	SubscribeReliable(ctx context.Context, subject, subscriberID string) (func(), error)
}

type ServiceEventBridgeOptions struct {
	ObserverQueueSize                int
	ControlQueueSize                 int
	ControlShards                    int
	ControlEnqueueTimeout            time.Duration
	SubscriberID                     string
	RequireOrderedReliableByShardKey bool
}

type controlEvent struct {
	ctx     context.Context
	request PublishRequest
	result  chan error
}

// ServiceEventBridge 是 ServiceContext 独占的事件运行时。观察事件允许在有界队列满时
// 丢弃；控制事件按 ShardKey 串行处理，并把本地或外发错误同步返回给调用方。
type ServiceEventBridge struct {
	stream *Stream

	observerQueue  chan PublishRequest
	controlQueues  []chan controlEvent
	controlTimeout time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	closed atomic.Bool
	once   sync.Once

	externalMu            sync.RWMutex
	external              ExternalPublisher
	subscriber            ExternalSubscriber
	reliableSubscriber    ReliableExternalSubscriber
	externalSubMu         sync.Mutex
	externalSubscriptions map[string]*externalSubscriptionRef
	outboxMu              sync.Mutex
	outbox                *outboxPublisher
	subscriberID          string
	// wantOrderedReliable 来自构造选项或显式 Require，表示意图；真正开启门禁必须 Ensure 成功。
	wantOrderedReliable bool
	// requireOrderedReliable 仅在 EnsureOrderedReliable 成功后置位。
	requireOrderedReliable atomic.Bool
	// orderedReliableEnsureErr 记录最近一次 Ensure 失败，避免 option 路径静默降级。
	// 存 *errorBox；nil box 表示尚无失败记录。
	orderedReliableEnsureErr atomic.Value
	dropped                  atomic.Uint64
	controlQueueTimeouts     atomic.Uint64
	publishFailures          atomic.Uint64
}

type errorBox struct{ err error }

func NewServiceEventBridge(stream *Stream, options ServiceEventBridgeOptions) *ServiceEventBridge {
	if stream == nil {
		stream = NewStream()
	}
	if options.ObserverQueueSize <= 0 {
		options.ObserverQueueSize = 256
	}
	if options.ControlQueueSize <= 0 {
		options.ControlQueueSize = 256
	}
	if options.ControlShards <= 0 {
		options.ControlShards = 8
	}
	if options.ControlEnqueueTimeout <= 0 {
		options.ControlEnqueueTimeout = defaultControlEnqueueTimeout
	}
	ctx, cancel := context.WithCancel(context.Background())
	b := &ServiceEventBridge{
		stream:              stream,
		observerQueue:       make(chan PublishRequest, options.ObserverQueueSize),
		controlQueues:       make([]chan controlEvent, options.ControlShards),
		controlTimeout:      options.ControlEnqueueTimeout,
		subscriberID:        options.SubscriberID,
		wantOrderedReliable: options.RequireOrderedReliableByShardKey,
		ctx:                 ctx,
		cancel:              cancel,
	}
	b.wg.Add(1)
	go b.runObserver()
	for i := range b.controlQueues {
		b.controlQueues[i] = make(chan controlEvent, options.ControlQueueSize)
		b.wg.Add(1)
		go b.runControl(b.controlQueues[i])
	}
	return b
}

func (b *ServiceEventBridge) Stream() *Stream {
	if b == nil {
		return nil
	}
	return b.stream
}

func (b *ServiceEventBridge) SetExternalPublisher(publisher ExternalPublisher) {
	if b == nil {
		return
	}
	b.externalMu.Lock()
	b.external = publisher
	b.subscriber, _ = publisher.(ExternalSubscriber)
	b.reliableSubscriber, _ = publisher.(ReliableExternalSubscriber)
	want := b.wantOrderedReliable
	b.externalMu.Unlock()
	// 构造时声明了 requirement：装配后立即 Ensure；失败暂存错误，外发路径 fail closed（禁止静默降级）。
	if want && publisher != nil {
		_ = b.ensureOrderedReliableOn(publisher)
	}
}

// RequireOrderedReliableByShardKey 声明本服务控制事件需要 ordered-reliable 能力。
// provider 不具备能力、未装配外部适配器时 fail closed；成功后开启空 ShardKey 发布门禁。
func (b *ServiceEventBridge) RequireOrderedReliableByShardKey() error {
	if b == nil || b.closed.Load() {
		return ErrServiceEventBridgeClosed
	}
	b.externalMu.Lock()
	b.wantOrderedReliable = true
	publisher := b.external
	b.externalMu.Unlock()
	if publisher == nil {
		err := ErrExternalProviderUnavailable
		b.storeOrderedReliableEnsureErr(err)
		return err
	}
	return b.ensureOrderedReliableOn(publisher)
}

func (b *ServiceEventBridge) storeOrderedReliableEnsureErr(err error) {
	b.orderedReliableEnsureErr.Store(errorBox{err: err})
}

func (b *ServiceEventBridge) loadOrderedReliableEnsureErr() error {
	v := b.orderedReliableEnsureErr.Load()
	if v == nil {
		return nil
	}
	box, ok := v.(errorBox)
	if !ok {
		return nil
	}
	return box.err
}

func (b *ServiceEventBridge) ensureOrderedReliableOn(publisher ExternalPublisher) error {
	ensurer, ok := publisher.(orderedReliableEnsurer)
	if !ok {
		b.requireOrderedReliable.Store(false)
		err := ErrOrderedReliableUnsupported
		b.storeOrderedReliableEnsureErr(err)
		return err
	}
	if err := ensurer.EnsureOrderedReliable(); err != nil {
		b.requireOrderedReliable.Store(false)
		b.storeOrderedReliableEnsureErr(err)
		return err
	}
	b.requireOrderedReliable.Store(true)
	b.storeOrderedReliableEnsureErr(nil)
	return nil
}

// RequiresOrderedReliable 报告是否已成功开启 ordered-reliable 发布门禁。
func (b *ServiceEventBridge) RequiresOrderedReliable() bool {
	if b == nil {
		return false
	}
	if b.requireOrderedReliable.Load() {
		return true
	}
	b.externalMu.RLock()
	publisher := b.external
	b.externalMu.RUnlock()
	if ensurer, ok := publisher.(orderedReliableEnsurer); ok {
		return ensurer.RequiresOrderedReliable()
	}
	return false
}

// orderedReliableRequiredButUnavailable 为 true 时：已声明 requirement 但 Ensure 未成功，外发必须 fail closed。
func (b *ServiceEventBridge) orderedReliableRequiredButUnavailable() error {
	if b == nil {
		return nil
	}
	if b.requireOrderedReliable.Load() {
		return nil
	}
	b.externalMu.RLock()
	want := b.wantOrderedReliable
	b.externalMu.RUnlock()
	if !want {
		return nil
	}
	if err := b.loadOrderedReliableEnsureErr(); err != nil {
		return err
	}
	return ErrOrderedReliableUnsupported
}

func (b *ServiceEventBridge) HasExternalPublisher() bool {
	if b == nil {
		return false
	}
	return b.externalPublisher() != nil
}

func (b *ServiceEventBridge) Subscribe(eventType string, handler Handler) (func(), error) {
	if b == nil || b.closed.Load() {
		return nil, ErrServiceEventBridgeClosed
	}
	return b.stream.Subscribe(eventType, handler)
}

// SubscribeControl 注册控制事件处理器。处理器错误会沿可靠外部订阅链返回，
// 使消息保留在 pending 中等待重试，而不是提前确认。
func (b *ServiceEventBridge) SubscribeControl(eventType string, handler ControlHandler) (func(), error) {
	if b == nil || b.closed.Load() {
		return nil, ErrServiceEventBridgeClosed
	}
	return b.stream.SubscribeControl(eventType, handler)
}

// SubscribeEvent 注册统一业务事件订阅。Subject 负责接入外部通道，EventType 是可选过滤条件。
func (b *ServiceEventBridge) SubscribeEvent(sub Subscription) (func(), error) {
	if b == nil || b.closed.Load() {
		return nil, ErrServiceEventBridgeClosed
	}
	if err := validateSubscription(sub); err != nil {
		return nil, err
	}
	match := func(env *Envelope) bool {
		if env == nil {
			return false
		}
		if sub.Subject != "" && env.Subject != sub.Subject {
			return false
		}
		if sub.EventType != "" && env.Type != sub.EventType {
			return false
		}
		return true
	}
	var localCancel func()
	var err error
	if sub.Reliable {
		handler := func(env *Envelope) error {
			if !match(env) {
				return nil
			}
			return sub.Handler(context.Background(), env)
		}
		if sub.EventType == "" {
			localCancel, err = b.stream.SubscribeAnyControl(handler)
		} else {
			localCancel, err = b.stream.SubscribeControl(sub.EventType, handler)
		}
	} else {
		handler := func(env *Envelope) {
			if match(env) {
				_ = sub.Handler(context.Background(), env)
			}
		}
		if sub.EventType == "" {
			localCancel, err = b.stream.SubscribeAny(handler)
		} else {
			localCancel, err = b.stream.Subscribe(sub.EventType, handler)
		}
	}
	if err != nil {
		return nil, err
	}
	var externalCancel func()
	if sub.Subject != "" && b.canSubscribeExternal(sub.Reliable) {
		externalCancel, err = b.subscribeExternalRef(context.Background(), sub.Reliable, sub.Subject)
		if err != nil {
			if localCancel != nil {
				localCancel()
			}
			return nil, err
		}
	}
	return combineCancels(externalCancel, localCancel), nil
}

func (b *ServiceEventBridge) canSubscribeExternal(reliable bool) bool {
	b.externalMu.RLock()
	defer b.externalMu.RUnlock()
	if reliable {
		return b.reliableSubscriber != nil && b.subscriberID != ""
	}
	return b.subscriber != nil
}

// SubscribeExternal 将指定主题交给已装配的外部事件适配器订阅。控制事件依赖
// 此订阅建立跨节点失效通道；未装配外部适配器时必须明确失败。
func (b *ServiceEventBridge) SubscribeExternal(ctx context.Context, subject string) (func(), error) {
	if b == nil || b.closed.Load() {
		return nil, ErrServiceEventBridgeClosed
	}
	b.externalMu.RLock()
	subscriber := b.subscriber
	b.externalMu.RUnlock()
	if subscriber == nil {
		return nil, ErrExternalProviderUnavailable
	}
	return subscriber.Subscribe(ctx, subject)
}

// SubscribeExternalControl 建立需要成功处理后才 ACK 的跨服务控制事件订阅。
func (b *ServiceEventBridge) SubscribeExternalControl(ctx context.Context, subject string) (func(), error) {
	if b == nil || b.closed.Load() {
		return nil, ErrServiceEventBridgeClosed
	}
	b.externalMu.RLock()
	subscriber := b.reliableSubscriber
	subscriberID := b.subscriberID
	b.externalMu.RUnlock()
	if subscriber == nil || subscriberID == "" {
		return nil, ErrExternalProviderUnavailable
	}
	return subscriber.SubscribeReliable(ctx, subject, subscriberID)
}

func (b *ServiceEventBridge) Publish(ctx context.Context, request PublishRequest) error {
	if b == nil || b.closed.Load() {
		return ErrServiceEventBridgeClosed
	}
	if request.Envelope == nil || request.Envelope.Type == "" {
		return ErrInvalidPublishRequest
	}
	if request.External && b.externalPublisher() == nil {
		return ErrExternalProviderUnavailable
	}
	// 已声明 requirement 但 Ensure 失败：禁止静默降级为无序/伪 key。
	if request.External {
		if err := b.orderedReliableRequiredButUnavailable(); err != nil {
			return err
		}
	}
	if request.External && b.RequiresOrderedReliable() && request.Envelope.ShardKey == "" {
		return ErrOrderingKeyRequired
	}
	if request.Class == ObserverDelivery {
		if !request.External && b.stream.SubscriberCount(request.Envelope.Type) == 0 {
			return nil
		}
		select {
		case b.observerQueue <- request:
		default:
			b.dropped.Add(1)
		}
		return nil
	}
	if request.Class != ControlDelivery {
		return ErrInvalidPublishRequest
	}

	job := controlEvent{ctx: ctx, request: request, result: make(chan error, 1)}
	queue := b.controlQueues[b.controlShard(request.Envelope)]
	select {
	case queue <- job:
		return b.waitControlResult(ctx, job)
	default:
	}

	timer := time.NewTimer(b.controlTimeout)
	defer timer.Stop()
	select {
	case queue <- job:
		return b.waitControlResult(ctx, job)
	case <-timer.C:
		b.controlQueueTimeouts.Add(1)
		return ErrControlQueueTimeout
	case <-ctx.Done():
		return ctx.Err()
	case <-b.ctx.Done():
		return ErrServiceEventBridgeClosed
	}
}

// UseOutbox 启用当前服务的可靠 Outbox 发布器。
func (b *ServiceEventBridge) UseOutbox(options OutboxOptions) error {
	publisher, err := newOutboxPublisher(b, options)
	if err != nil {
		return err
	}
	b.outboxMu.Lock()
	old := b.outbox
	b.outbox = publisher
	b.outboxMu.Unlock()
	if old != nil {
		old.close()
	}
	return nil
}

// NotifyOutbox 唤醒 Outbox 发布器尽快扫描本服务待发布事件。
func (b *ServiceEventBridge) NotifyOutbox() {
	if b == nil {
		return
	}
	b.outboxMu.Lock()
	publisher := b.outbox
	b.outboxMu.Unlock()
	if publisher != nil {
		publisher.notifyNow()
	}
}

func (b *ServiceEventBridge) waitControlResult(ctx context.Context, job controlEvent) error {
	select {
	case err := <-job.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-b.ctx.Done():
		return ErrServiceEventBridgeClosed
	}
}

func (b *ServiceEventBridge) ObserverDropped() uint64 {
	if b == nil {
		return 0
	}
	return b.dropped.Load()
}

func (b *ServiceEventBridge) ControlQueueTimeouts() uint64 {
	if b == nil {
		return 0
	}
	return b.controlQueueTimeouts.Load()
}

func (b *ServiceEventBridge) Close(ctx context.Context) error {
	if b == nil {
		return nil
	}
	b.once.Do(func() {
		b.closed.Store(true)
		b.outboxMu.Lock()
		outbox := b.outbox
		b.outbox = nil
		b.outboxMu.Unlock()
		if outbox != nil {
			outbox.close()
		}
		b.closeExternalSubscriptions()
		b.cancel()
	})
	done := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *ServiceEventBridge) runObserver() {
	defer b.wg.Done()
	for {
		select {
		case <-b.ctx.Done():
			return
		case request := <-b.observerQueue:
			if err := b.deliver(context.Background(), request); err != nil {
				b.publishFailures.Add(1)
			}
		}
	}
}

func (b *ServiceEventBridge) runControl(queue <-chan controlEvent) {
	defer b.wg.Done()
	for {
		select {
		case <-b.ctx.Done():
			return
		case job := <-queue:
			err := b.deliver(job.ctx, job.request)
			if err != nil {
				b.publishFailures.Add(1)
			}
			job.result <- err
		}
	}
}

func (b *ServiceEventBridge) deliver(ctx context.Context, request PublishRequest) error {
	env := cloneEnvelope(request.Envelope)
	if request.BuildData != nil {
		data, err := request.BuildData()
		if err != nil {
			return err
		}
		env.Data = data
		if len(data) > 0 && env.DataContentType == "" {
			env.DataContentType = "application/json"
		}
	}
	if err := b.stream.Publish(ctx, env); err != nil {
		return err
	}
	if request.Class == ControlDelivery {
		if err := b.stream.PublishControl(ctx, env); err != nil {
			return err
		}
	}
	if request.External {
		publisher := b.externalPublisher()
		if publisher == nil {
			return ErrExternalProviderUnavailable
		}
		return publisher.Publish(ctx, request.Subject, env)
	}
	return nil
}

func (b *ServiceEventBridge) externalPublisher() ExternalPublisher {
	b.externalMu.RLock()
	publisher := b.external
	b.externalMu.RUnlock()
	return publisher
}

func (b *ServiceEventBridge) controlShard(env *Envelope) int {
	key := env.ShardKey
	if key == "" {
		key = env.Type + ":" + env.Subject
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(key))
	return int(hash.Sum32() % uint32(len(b.controlQueues)))
}

func cloneEnvelope(source *Envelope) *Envelope {
	copyValue := *source
	copyValue.Data = append([]byte(nil), source.Data...)
	return &copyValue
}
