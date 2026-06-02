package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// forwardedNotice is the JSON payload sent to peer nodes' relay endpoint.
type forwardedNotice struct {
	RoutePath string          `json:"route_path"`
	Hash      uint64          `json:"hash"`
	Message   json.RawMessage `json:"message"`
}

// subscriptionSummaryPayload is the JSON payload sent to peer nodes
// when local subscription state changes.
type subscriptionSummaryPayload struct {
	RoutePath string `json:"route_path"`
	Hash      uint64 `json:"hash"`
	NodeID    string `json:"node_id"`
	Active    bool   `json:"active"`
}

// CrossNodeNoticeBroker implements types.ICrossNodeForwarder by forwarding
// WebSocket notices to peer nodes via HTTP and tracking peer subscriptions.
//
// When ForwardNotice is called on node A, it:
//  1. Looks up peer subscriptions in the registry.
//  2. For each peer node that has subscribers for routePath+hash, sends an
//     HTTP POST to /api/servermanage/ws/notice.
//  3. The receiving node's relay endpoint calls RouterInfo.ExecuteLocalNotice.
type CrossNodeNoticeBroker struct {
	provider    DiscoveryProvider
	serviceName string
	localNodeID string
	httpClient  *http.Client

	// peer subscription registry: routePath -> hash -> nodeID set
	subMu sync.RWMutex
	subs  map[string]map[uint64]map[string]bool

	stopped bool
	stopMu  sync.Mutex
}

// NewCrossNodeNoticeBroker creates a broker backed by the given provider.
func NewCrossNodeNoticeBroker(provider DiscoveryProvider, serviceName, localNodeID string) *CrossNodeNoticeBroker {
	return &CrossNodeNoticeBroker{
		provider:    provider,
		serviceName: serviceName,
		localNodeID: localNodeID,
		httpClient:  &http.Client{Timeout: 3 * time.Second},
		subs:        make(map[string]map[uint64]map[string]bool),
	}
}

// ForwardNotice implements types.ICrossNodeForwarder.
// It checks which peer nodes have subscribed to the given routePath+hash and
// forwards the message to them asynchronously.
func (b *CrossNodeNoticeBroker) ForwardNotice(ctx context.Context, routePath string, hash uint64, message interface{}) {
	b.stopMu.Lock()
	if b.stopped {
		b.stopMu.Unlock()
		return
	}
	b.stopMu.Unlock()

	// Find peer nodes with subscriptions.
	peers := b.peerNodesForHash(routePath, hash)
	if len(peers) == 0 {
		return
	}

	msgBytes, err := json.Marshal(message)
	if err != nil {
		logx.Errorf("CrossNodeBroker: marshal message: %v", err)
		return
	}
	payload := &forwardedNotice{
		RoutePath: routePath,
		Hash:      hash,
		Message:   msgBytes,
	}

	for _, nodeID := range peers {
		go b.sendNoticeToPeer(nodeID, payload)
	}
}

// OnSubscriptionChange implements types.ICrossNodeForwarder.
// It broadcasts local subscription state to all running peer nodes.
func (b *CrossNodeNoticeBroker) OnSubscriptionChange(routePath string, hash uint64, active bool) {
	b.stopMu.Lock()
	if b.stopped {
		b.stopMu.Unlock()
		return
	}
	b.stopMu.Unlock()

	summary := &subscriptionSummaryPayload{
		RoutePath: routePath,
		Hash:      hash,
		NodeID:    b.localNodeID,
		Active:    active,
	}
	// Push to all running peers.
	nodes, err := b.provider.List(context.Background(), b.serviceName, NodeStatusRunning)
	if err != nil {
		logx.Errorf("CrossNodeBroker: list nodes for subscription sync: %v", err)
		return
	}
	for _, n := range nodes {
		if n.ID == b.localNodeID {
			continue
		}
		go b.sendSummaryToPeer(n, summary)
	}
}

// UpdatePeerSubscription updates the local registry when a peer node notifies
// us of a subscription change. Called by the relay endpoint.
func (b *CrossNodeNoticeBroker) UpdatePeerSubscription(routePath string, hash uint64, nodeID string, active bool) {
	b.subMu.Lock()
	defer b.subMu.Unlock()
	if active {
		if b.subs[routePath] == nil {
			b.subs[routePath] = make(map[uint64]map[string]bool)
		}
		if b.subs[routePath][hash] == nil {
			b.subs[routePath][hash] = make(map[string]bool)
		}
		b.subs[routePath][hash][nodeID] = true
	} else {
		if b.subs[routePath] != nil && b.subs[routePath][hash] != nil {
			delete(b.subs[routePath][hash], nodeID)
		}
	}
}

// DrainAndStop broadcasts subscription-removed events for all local subscriptions
// and marks the broker as stopped. Called by ServiceContext.Stop().
func (b *CrossNodeNoticeBroker) DrainAndStop(ctx context.Context) {
	b.stopMu.Lock()
	b.stopped = true
	b.stopMu.Unlock()

	// The caller is responsible for removing local subscriptions; we just
	// broadcast the removal summaries so peers can clean up their registries.
	b.subMu.RLock()
	type pathHash struct{ path string; hash uint64 }
	toRemove := make([]pathHash, 0)
	for path, hashes := range b.subs {
		for hash := range hashes {
			if hashes[hash][b.localNodeID] {
				toRemove = append(toRemove, pathHash{path, hash})
			}
		}
	}
	b.subMu.RUnlock()

	for _, item := range toRemove {
		b.OnSubscriptionChange(item.path, item.hash, false)
	}
}

// peerNodesForHash returns node IDs that have subscribers for routePath+hash,
// excluding this node.
func (b *CrossNodeNoticeBroker) peerNodesForHash(routePath string, hash uint64) []string {
	b.subMu.RLock()
	defer b.subMu.RUnlock()
	hashes, ok := b.subs[routePath]
	if !ok {
		return nil
	}
	nodes, ok := hashes[hash]
	if !ok {
		return nil
	}
	result := make([]string, 0, len(nodes))
	for id := range nodes {
		if id != b.localNodeID {
			result = append(result, id)
		}
	}
	return result
}

// sendNoticeToPeer looks up the peer node address and POSTs the forwarded notice.
func (b *CrossNodeNoticeBroker) sendNoticeToPeer(nodeID string, payload *forwardedNotice) {
	node, err := b.provider.Get(context.Background(), nodeID)
	if err != nil {
		logx.Errorf("CrossNodeBroker: get node %s: %v", nodeID, err)
		return
	}
	if err := b.post(node, "/api/servermanage/ws/notice", payload); err != nil {
		logx.Errorf("CrossNodeBroker: forward notice to %s: %v", nodeID, err)
	}
}

// sendSummaryToPeer POSTs a subscription summary change to a peer node.
func (b *CrossNodeNoticeBroker) sendSummaryToPeer(node *NodeInfo, payload *subscriptionSummaryPayload) {
	if err := b.post(node, "/api/servermanage/ws/subscription", payload); err != nil {
		logx.Errorf("CrossNodeBroker: send subscription summary to %s: %v", node.ID, err)
	}
}

func (b *CrossNodeNoticeBroker) post(node *NodeInfo, path string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	url := fmt.Sprintf("http://%s:%d%s", node.Address, node.Port, path)
	resp, err := b.httpClient.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
