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

// ConsistentHashBalancer 使用 rendezvous hash 将相同 HashKey 稳定路由到同一节点。
// 候选顺序不会改变 owner；新增节点时只有迁移到新节点的 key 会改变 owner。
type ConsistentHashBalancer struct{}

func NewConsistentHashBalancer() *ConsistentHashBalancer { return &ConsistentHashBalancer{} }

func (b *ConsistentHashBalancer) Pick(_ context.Context, candidates []*NodeInfo, hint BalanceHint) (*NodeInfo, error) {
	if len(candidates) == 0 {
		return nil, ErrEmptyCandidates
	}
	var selected *NodeInfo
	var selectedScore uint64
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		h := fnv.New64a()
		identity := consistentHashNodeIdentity(candidate)
		fmt.Fprint(h, hint.HashKey, "\x00", identity)
		score := h.Sum64()
		if selected == nil || score > selectedScore || score == selectedScore && identity < consistentHashNodeIdentity(selected) {
			selected = candidate
			selectedScore = score
		}
	}
	if selected == nil {
		return nil, ErrEmptyCandidates
	}
	return selected, nil
}

// consistentHashNodeIdentity 使用副本的稳定网络端点，不使用每次启动都变化的 NodeInfo.ID。
// 旧测试/进程内节点没有端点时回退 ID，保持兼容。
func consistentHashNodeIdentity(node *NodeInfo) string {
	if node == nil {
		return ""
	}
	if node.Address == "" && node.Port == 0 && node.GRPCPort == 0 {
		return node.ID
	}
	return fmt.Sprintf("%s\x00%s\x00%d\x00%d", node.ServiceName, node.Address, node.Port, node.GRPCPort)
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
