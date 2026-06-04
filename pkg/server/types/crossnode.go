package types

import (
	"context"
	"sync"
)

// ICrossNodeForwarder is the interface implemented by the cluster package
// to forward WebSocket notices to peer nodes and track subscription changes.
type ICrossNodeForwarder interface {
	// ForwardNotice sends message to peer nodes that have subscribers for
	// the given routePath. Implementations must be non-blocking (fire-and-forget).
	ForwardNotice(ctx context.Context, routePath string, hash uint64, message interface{})

	// OnSubscriptionChange informs the forwarder that a local subscription
	// was added (active=true) or removed (active=false) for routePath+hash.
	OnSubscriptionChange(routePath string, hash uint64, active bool)

	// DrainAndStop signals that this node is shutting down: broadcast
	// subscription-removed events for all current subscriptions and stop
	// forwarding new notices.
	DrainAndStop(ctx context.Context)
}

var (
	globalCrossNodeForwarder   ICrossNodeForwarder
	globalCrossNodeForwarderMu sync.RWMutex
)

// SetCrossNodeForwarder registers a global forwarder used by RouterInfo to
// broadcast WebSocket notices to peer nodes. Called by ServiceContext during Start().
func SetCrossNodeForwarder(f ICrossNodeForwarder) {
	globalCrossNodeForwarderMu.Lock()
	globalCrossNodeForwarder = f
	globalCrossNodeForwarderMu.Unlock()
}

// GetCrossNodeForwarder returns the active global forwarder, or nil if none is set.
func GetCrossNodeForwarder() ICrossNodeForwarder {
	globalCrossNodeForwarderMu.RLock()
	f := globalCrossNodeForwarder
	globalCrossNodeForwarderMu.RUnlock()
	return f
}
