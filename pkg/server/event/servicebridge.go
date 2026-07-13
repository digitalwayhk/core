package event

import (
	"context"
	"errors"
	"hash/fnv"
	"sync"
	"sync/atomic"
)

var (
	ErrExternalProviderUnavailable = errors.New("event external provider unavailable")
	ErrServiceEventBridgeClosed    = errors.New("service event bridge closed")
	ErrInvalidPublishRequest       = errors.New("invalid event publish request")
)

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

type ServiceEventBridgeOptions struct {
	ObserverQueueSize int
	ControlQueueSize  int
	ControlShards     int
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

	observerQueue chan PublishRequest
	controlQueues []chan controlEvent

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	closed atomic.Bool
	once   sync.Once

	externalMu sync.RWMutex
	external   ExternalPublisher
	subscriber ExternalSubscriber
	dropped    atomic.Uint64
}

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
	ctx, cancel := context.WithCancel(context.Background())
	b := &ServiceEventBridge{
		stream:        stream,
		observerQueue: make(chan PublishRequest, options.ObserverQueueSize),
		controlQueues: make([]chan controlEvent, options.ControlShards),
		ctx:           ctx,
		cancel:        cancel,
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
	b.externalMu.Unlock()
}

func (b *ServiceEventBridge) Subscribe(eventType string, handler Handler) (func(), error) {
	if b == nil || b.closed.Load() {
		return nil, ErrServiceEventBridgeClosed
	}
	return b.stream.Subscribe(eventType, handler)
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
	case <-ctx.Done():
		return ctx.Err()
	case <-b.ctx.Done():
		return ErrServiceEventBridgeClosed
	}
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

func (b *ServiceEventBridge) Close(ctx context.Context) error {
	if b == nil {
		return nil
	}
	b.once.Do(func() {
		b.closed.Store(true)
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
			_ = b.deliver(context.Background(), request)
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
			job.result <- b.deliver(job.ctx, job.request)
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
