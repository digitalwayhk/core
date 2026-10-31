package event

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/zeromicro/go-zero/core/logx"
)

// Handler is a function that processes a received event envelope.
type Handler func(env *Envelope)

// ControlHandler 返回错误时，外部可靠消费不得 ACK 当前消息。
type ControlHandler func(env *Envelope) error

// Stream is a simple in-process event bus that fans out events to registered handlers.
// It is used when no external MQ provider is configured (mode=off or auto without connectivity).
type Stream struct {
	mu          sync.RWMutex
	handlers    map[string][]Handler // keyed by event Type
	controls    map[string][]*controlSubscription
	anyHandlers []Handler
	anyControls []*controlSubscription
}

// NewStream returns an initialised local event stream.
func NewStream() *Stream {
	return &Stream{handlers: make(map[string][]Handler), controls: make(map[string][]*controlSubscription)}
}

// controlSubscription 的入口标记只保存在Core内，不进入Envelope或业务payload。
type controlSubscription struct {
	handler    func(context.Context, *Envelope) error
	brokerOnly bool
	subject    string
	group      string
}

// SubscribeControl 注册必须成功处理后才能确认的控制事件Handler；零值行为保持两个入口都接收。
func (s *Stream) SubscribeControl(eventType string, handler ControlHandler) (func(), error) {
	if handler == nil {
		return nil, errors.New("event control handler is nil")
	}
	return s.subscribeControl(eventType, false, false, "", "", func(_ context.Context, e *Envelope) error { return handler(e) })
}

// SubscribeAnyControl 注册接收所有控制事件的Handler，调用方可自行按Subject/Type过滤。
func (s *Stream) SubscribeAnyControl(handler ControlHandler) (func(), error) {
	if handler == nil {
		return nil, errors.New("event control handler is nil")
	}
	return s.subscribeControl("", true, false, "", "", func(_ context.Context, e *Envelope) error { return handler(e) })
}

func (s *Stream) subscribeControl(kind string, anyType, brokerOnly bool, subject, group string, handler func(context.Context, *Envelope) error) (func(), error) {
	if handler == nil {
		return nil, errors.New("event control handler is nil")
	}
	entry := &controlSubscription{handler: handler, brokerOnly: brokerOnly, subject: subject, group: group}
	s.mu.Lock()
	if anyType {
		s.anyControls = append(s.anyControls, entry)
	} else {
		s.controls[kind] = append(s.controls[kind], entry)
	}
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			list := s.controls[kind]
			if anyType {
				list = s.anyControls
			}
			updated := make([]*controlSubscription, 0, len(list))
			for _, current := range list {
				if current != entry {
					updated = append(updated, current)
				}
			}
			if anyType {
				s.anyControls = updated
			} else {
				s.controls[kind] = updated
			}
		})
	}, nil
}

// Subscribe registers handler to be called for events of the given type.
// Returns a cancel function to unsubscribe and a nil error (reserved for future use).
func (s *Stream) Subscribe(eventType string, handler Handler) (func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[eventType] = append(s.handlers[eventType], handler)
	key := fmt.Sprintf("%p", handler)
	cancel := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		list := s.handlers[eventType]
		updated := make([]Handler, 0, len(list))
		for _, h := range list {
			if fmt.Sprintf("%p", h) != key {
				updated = append(updated, h)
			}
		}
		s.handlers[eventType] = updated
	}
	return cancel, nil
}

// SubscribeAny 注册接收所有观察事件的 Handler，调用方可自行按 Subject/Type 过滤。
func (s *Stream) SubscribeAny(handler Handler) (func(), error) {
	if handler == nil {
		return nil, errors.New("event handler is nil")
	}
	s.mu.Lock()
	s.anyHandlers = append(s.anyHandlers, handler)
	key := fmt.Sprintf("%p", handler)
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		list := s.anyHandlers
		updated := make([]Handler, 0, len(list))
		for _, h := range list {
			if fmt.Sprintf("%p", h) != key {
				updated = append(updated, h)
			}
		}
		s.anyHandlers = updated
		s.mu.Unlock()
	}, nil
}

// Publish delivers the envelope to all handlers registered for its Type.
// Delivery is synchronous; use goroutines in handlers for async processing.
func (s *Stream) Publish(_ context.Context, env *Envelope) error {
	if env == nil {
		return nil
	}
	s.mu.RLock()
	handlers := make([]Handler, len(s.handlers[env.Type]))
	copy(handlers, s.handlers[env.Type])
	handlers = append(handlers, s.anyHandlers...)
	s.mu.RUnlock()

	for _, h := range handlers {
		func() {
			defer func() {
				if recover() != nil {
					logx.Errorw("event_handler_panic", logx.Field("event_type", env.Type))
				}
			}()
			h(env)
		}()
	}
	return nil
}

// PublishControl 同步执行全部控制 Handler，并返回聚合错误。
func (s *Stream) PublishControl(ctx context.Context, env *Envelope) error {
	return s.publishControl(ctx, env, false, "", "")
}

// publishBrokerControl 仅由外部可靠适配器调用；业务Envelope和Source无法声明此入口。
func (s *Stream) publishBrokerControl(ctx context.Context, env *Envelope, subject, group string) error {
	return s.publishControl(ctx, env, true, subject, group)
}

func (s *Stream) publishControl(ctx context.Context, env *Envelope, broker bool, subject, group string) error {
	if env == nil {
		return nil
	}
	s.mu.RLock()
	handlers := append([]*controlSubscription(nil), s.controls[env.Type]...)
	handlers = append(handlers, s.anyControls...)
	s.mu.RUnlock()
	var failures []error
	for _, entry := range handlers {
		if entry.brokerOnly && (!broker || entry.subject != subject || entry.group != group) {
			continue
		}
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					failures = append(failures, fmt.Errorf("event control handler panic: %v", recovered))
				}
			}()
			if err := entry.handler(ctx, env); err != nil {
				failures = append(failures, err)
			}
		}()
	}
	return errors.Join(failures...)
}

// SubscriberCount 返回指定事件类型当前的本地订阅者数量。
func (s *Stream) SubscriberCount(eventType string) int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	count := len(s.handlers[eventType])
	s.mu.RUnlock()
	return count
}
