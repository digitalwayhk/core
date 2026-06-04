package cluster

import (
	"context"
	"fmt"
	"hash/fnv"
	"sync"
	"sync/atomic"
)

// LoadBalancer selects a single node from a candidate list.
type LoadBalancer interface {
	// Pick returns a single node from candidates.
	// Returns ErrEmptyCandidates when the list is empty.
	Pick(ctx context.Context, candidates []*NodeInfo, hint BalanceHint) (*NodeInfo, error)
}

// BalanceHint carries optional data used by certain strategies (e.g. hash key).
type BalanceHint struct {
	// HashKey is used by consistent-hash balancing.
	HashKey string
	// PreferLocal makes local-first strategy prefer the current process.
	PreferLocal bool
	// LocalServiceName is used by local-first to identify the current node.
	LocalNodeID string
}

// ---- local-first ----

// LocalFirstBalancer prefers the node whose ID matches LocalNodeID, falling
// back to round-robin among remaining candidates.
type LocalFirstBalancer struct {
	rr *RoundRobinBalancer
}

func NewLocalFirstBalancer() *LocalFirstBalancer {
	return &LocalFirstBalancer{rr: NewRoundRobinBalancer()}
}

func (b *LocalFirstBalancer) Pick(_ context.Context, candidates []*NodeInfo, hint BalanceHint) (*NodeInfo, error) {
	if len(candidates) == 0 {
		return nil, ErrEmptyCandidates
	}
	if hint.LocalNodeID != "" {
		for _, n := range candidates {
			if n.ID == hint.LocalNodeID {
				return n, nil
			}
		}
	}
	return b.rr.Pick(context.Background(), candidates, hint)
}

// ---- round-robin ----

// RoundRobinBalancer distributes calls uniformly across all candidates.
type RoundRobinBalancer struct {
	counter atomic.Uint64
}

func NewRoundRobinBalancer() *RoundRobinBalancer { return &RoundRobinBalancer{} }

func (b *RoundRobinBalancer) Pick(_ context.Context, candidates []*NodeInfo, _ BalanceHint) (*NodeInfo, error) {
	if len(candidates) == 0 {
		return nil, ErrEmptyCandidates
	}
	idx := b.counter.Add(1) - 1
	return candidates[idx%uint64(len(candidates))], nil
}

// ---- consistent-hash ----

// ConsistentHashBalancer routes calls with the same HashKey to the same node
// (modulo changes to the candidate list).
type ConsistentHashBalancer struct{}

func NewConsistentHashBalancer() *ConsistentHashBalancer { return &ConsistentHashBalancer{} }

func (b *ConsistentHashBalancer) Pick(_ context.Context, candidates []*NodeInfo, hint BalanceHint) (*NodeInfo, error) {
	if len(candidates) == 0 {
		return nil, ErrEmptyCandidates
	}
	h := fnv.New32a()
	fmt.Fprint(h, hint.HashKey)
	idx := h.Sum32() % uint32(len(candidates))
	return candidates[idx], nil
}

// ---- weighted ----

// WeightedBalancer picks nodes proportionally to their Weight field.
// Weight=0 nodes are skipped.
type WeightedBalancer struct {
	mu      sync.Mutex
	counter uint64
}

func NewWeightedBalancer() *WeightedBalancer { return &WeightedBalancer{} }

func (b *WeightedBalancer) Pick(_ context.Context, candidates []*NodeInfo, _ BalanceHint) (*NodeInfo, error) {
	// Build expanded list respecting weights.
	expanded := make([]*NodeInfo, 0, len(candidates)*2)
	for _, n := range candidates {
		w := n.Weight
		if w <= 0 {
			w = 1
		}
		for i := 0; i < w; i++ {
			expanded = append(expanded, n)
		}
	}
	if len(expanded) == 0 {
		return nil, ErrEmptyCandidates
	}
	b.mu.Lock()
	idx := b.counter % uint64(len(expanded))
	b.counter++
	b.mu.Unlock()
	return expanded[idx], nil
}
