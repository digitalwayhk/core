// Package transport provides a unified interface for all internal service-to-service
// communication channels. Concrete implementations (HTTP, Socket, gRPC, MQ) live in
// sub-packages and are wired together by a TransportSelector.
package transport

import (
	"context"

	"github.com/digitalwayhk/core/pkg/server/types"
)

// Transport is the common interface for every inter-service transport channel.
type Transport interface {
	// Name returns a stable, unique identifier for this transport (e.g. "grpc", "http").
	Name() string

	// Start initialises and starts any listeners required by the transport.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the transport within the given context.
	Stop(ctx context.Context) error

	// Supports returns true when this transport can handle the given payload / target.
	Supports(ctx context.Context, payload *types.PayLoad, target string) bool

	// Send serialises and delivers the payload to the target address, returning the raw response.
	Send(ctx context.Context, payload *types.PayLoad, target string) ([]byte, error)

	// Health performs a lightweight health check against the target address.
	Health(ctx context.Context, target string) error
}

// TransportSelector chooses the most appropriate Transport for a given call,
// applying fallback logic when the primary transport is unavailable.
type TransportSelector interface {
	// Select returns the best Transport for the payload + target, or an error
	// when no healthy transport is available.
	Select(ctx context.Context, payload *types.PayLoad, target string) (Transport, error)
}
