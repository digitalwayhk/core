package transport

import (
	"context"
	"errors"
	"fmt"

	"github.com/digitalwayhk/core/pkg/server/types"
)

// ErrNoTransport is returned when no healthy transport can be found for a call.
var ErrNoTransport = errors.New("transport: no healthy transport available")

// DefaultSelector selects the primary transport and falls back through the
// supplied list when the primary is unhealthy or incapable.
type DefaultSelector struct {
	primary  Transport
	fallback []Transport
}

// NewDefaultSelector creates a selector with a primary and optional fallbacks.
func NewDefaultSelector(primary Transport, fallback ...Transport) *DefaultSelector {
	return &DefaultSelector{primary: primary, fallback: fallback}
}

// Select returns the first healthy Transport that supports the payload / target.
// It tries the primary first, then each fallback in order.
func (s *DefaultSelector) Select(ctx context.Context, payload *types.PayLoad, target string) (Transport, error) {
	candidates := append([]Transport{s.primary}, s.fallback...)
	for _, t := range candidates {
		if t == nil {
			continue
		}
		if !t.Supports(ctx, payload, target) {
			continue
		}
		if err := t.Health(ctx, target); err == nil {
			return t, nil
		}
	}
	return nil, fmt.Errorf("%w: target=%s", ErrNoTransport, target)
}

// SendWithFallback sends a payload using the selector, automatically retrying
// through the fallback chain on transport errors.
func SendWithFallback(ctx context.Context, sel TransportSelector, payload *types.PayLoad, target string) ([]byte, error) {
	t, err := sel.Select(ctx, payload, target)
	if err != nil {
		return nil, err
	}
	return t.Send(ctx, payload, target)
}
