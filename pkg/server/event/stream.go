package event

import (
	"context"
	"fmt"
	"sync"

	"github.com/zeromicro/go-zero/core/logx"
)

// Handler is a function that processes a received event envelope.
type Handler func(env *Envelope)

// Stream is a simple in-process event bus that fans out events to registered handlers.
// It is used when no external MQ provider is configured (mode=off or auto without connectivity).
type Stream struct {
	mu       sync.RWMutex
	handlers map[string][]Handler // keyed by event Type
}

// NewStream returns an initialised local event stream.
func NewStream() *Stream {
	return &Stream{handlers: make(map[string][]Handler)}
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

// Publish delivers the envelope to all handlers registered for its Type.
// Delivery is synchronous; use goroutines in handlers for async processing.
func (s *Stream) Publish(_ context.Context, env *Envelope) error {
	if env == nil {
		return nil
	}
	s.mu.RLock()
	handlers := make([]Handler, len(s.handlers[env.Type]))
	copy(handlers, s.handlers[env.Type])
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
