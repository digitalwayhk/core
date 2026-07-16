package transport

import (
	"context"
	"errors"
	"time"

	"github.com/digitalwayhk/core/pkg/server/types"
)

// ErrNoTransport is returned when no healthy transport can be found for a call.
var ErrNoTransport = errors.New("transport: no healthy transport available")

// DefaultSelector selects the primary transport and falls back through the
// supplied list when the primary is unhealthy or incapable.
type DefaultSelector struct {
	primary  Transport
	fallback []Transport
	stats    *Stats
}

// Stop 关闭本 Selector 持有的客户端传输资源。每个传输实例最多关闭一次。
func (s *DefaultSelector) Stop(ctx context.Context) error {
	seen := make(map[Transport]struct{}, 1+len(s.fallback))
	var firstErr error
	for _, candidate := range append([]Transport{s.primary}, s.fallback...) {
		if candidate == nil {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		if err := candidate.Stop(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// NewDefaultSelector creates a selector with a primary and optional fallbacks.
func NewDefaultSelector(primary Transport, fallback ...Transport) *DefaultSelector {
	return &DefaultSelector{primary: primary, fallback: fallback}
}

// SetStats 注入当前 ServiceContext 独占的指标收集器。
func (s *DefaultSelector) SetStats(stats *Stats) {
	s.stats = stats
}

// Select returns the first healthy Transport that supports the payload / target.
// It tries the primary first, then each fallback in order.
func (s *DefaultSelector) Select(ctx context.Context, payload *types.PayLoad, endpoints TransportEndpoints) (Selection, error) {
	candidates := append([]Transport{s.primary}, s.fallback...)
	for index, t := range candidates {
		if err := ctx.Err(); err != nil {
			return Selection{}, err
		}
		if t == nil {
			continue
		}
		endpoint := endpoints.For(t.Name())
		if endpoint == "" {
			continue
		}
		if !t.Supports(ctx, payload, endpoint) {
			continue
		}
		healthErr := t.Health(ctx, endpoint)
		if err := ctx.Err(); err != nil {
			return Selection{}, err
		}
		if healthErr == nil {
			s.stats.recordSelection(t.Name(), index > 0)
			return Selection{Transport: t, Endpoint: endpoint}, nil
		}
	}
	return Selection{}, ErrNoTransport
}

// SelectWithRetry 只重试发送前的协议选择和健康检查。
// attempts 小于 1 时按一次处理；等待过程响应 context 取消。
func SelectWithRetry(ctx context.Context, sel TransportSelector, payload *types.PayLoad, endpoints TransportEndpoints, attempts int, delay time.Duration) (Selection, error) {
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return Selection{}, err
		}
		selection, err := sel.Select(ctx, payload, endpoints)
		if contextErr := ctx.Err(); contextErr != nil {
			return Selection{}, contextErr
		}
		if err == nil {
			return selection, nil
		}
		lastErr = err
		if attempt == attempts-1 || delay <= 0 {
			continue
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Selection{}, ctx.Err()
		case <-timer.C:
		}
	}
	return Selection{}, lastErr
}

// Send 在发送前选择一次协议，并且只调用一次已选 Transport.Send。
// Send 返回任何错误时都不会重新选择协议或重放请求。
func Send(ctx context.Context, sel TransportSelector, payload *types.PayLoad, endpoints TransportEndpoints) ([]byte, error) {
	selection, err := sel.Select(ctx, payload, endpoints)
	if err != nil {
		return nil, err
	}
	return SendSelection(ctx, sel, selection, payload)
}

// SendSelection 执行一次已经完成预检的选择结果并记录发送结果。
func SendSelection(ctx context.Context, sel TransportSelector, selection Selection, payload *types.PayLoad) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result, err := selection.Transport.Send(ctx, payload, selection.Endpoint)
	if selector, ok := sel.(*DefaultSelector); ok {
		selector.stats.recordSend(err)
	}
	return result, err
}
