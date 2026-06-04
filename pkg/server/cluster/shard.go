package cluster

import (
	"errors"
	"fmt"
)

// ShardMode controls how shard-key routing decisions are made.
type ShardMode string

const (
	// ShardModeExact requires an exact match between the payload shard key value and a node's metadata.
	ShardModeExact ShardMode = "exact"
	// ShardModeGroup routes to a named group of nodes sharing a common metadata tag.
	ShardModeGroup ShardMode = "group"
	// ShardModeHash distributes using consistent hash over the shard key value.
	ShardModeHash ShardMode = "hash"
	// ShardModeOptional applies the rule when the shard key is present; otherwise uses all candidates.
	ShardModeOptional ShardMode = "optional"
)

// MissingKeyPolicy controls what happens when a required shard key is absent.
type MissingKeyPolicy string

const (
	MissingKeyPolicyError    MissingKeyPolicy = "error"
	MissingKeyPolicyFallback MissingKeyPolicy = "fallback"
)

// EmptyCandidatePolicy controls what happens when no candidates match the shard rule.
type EmptyCandidatePolicy string

const (
	EmptyCandidatePolicyError    EmptyCandidatePolicy = "error"
	EmptyCandidatePolicyFallback EmptyCandidatePolicy = "fallback"
)

// ServiceShardRule defines a shard-routing rule for a specific service.
type ServiceShardRule struct {
	// MetadataKey is the node metadata key to match against (e.g. "accountTypeid").
	MetadataKey string

	// PayloadKey is the key looked up in the payload's shard parameters.
	PayloadKey string

	// Mode determines the routing algorithm.
	Mode ShardMode

	// MissingKey controls behaviour when PayloadKey is not present in the payload.
	MissingKey MissingKeyPolicy

	// EmptyCandidate controls behaviour when no nodes match the rule.
	EmptyCandidate EmptyCandidatePolicy
}

// ServiceShardRouter filters a node candidate list according to configured shard rules.
type ServiceShardRouter struct {
	rules map[string]ServiceShardRule // key = serviceName
}

// NewServiceShardRouter creates an empty router.
func NewServiceShardRouter() *ServiceShardRouter {
	return &ServiceShardRouter{rules: make(map[string]ServiceShardRule)}
}

// AddRule registers a shard rule for the given service.
func (r *ServiceShardRouter) AddRule(serviceName string, rule ServiceShardRule) {
	r.rules[serviceName] = rule
}

// Filter applies the shard rule for serviceName (if any) to the candidate list.
// params are caller-supplied key-value pairs from the request (e.g. payload metadata).
// Returns the filtered candidate list, which may equal the input if no rule applies.
func (r *ServiceShardRouter) Filter(serviceName string, candidates []*NodeInfo, params map[string]string) ([]*NodeInfo, error) {
	rule, ok := r.rules[serviceName]
	if !ok {
		return candidates, nil
	}

	val, hasKey := params[rule.PayloadKey]
	if !hasKey {
		switch rule.MissingKey {
		case MissingKeyPolicyError:
			return nil, fmt.Errorf("cluster shard: required key %q missing for service %q: %w",
				rule.PayloadKey, serviceName, errors.New("missing shard key"))
		default:
			// fallback: use all candidates
			return candidates, nil
		}
	}

	if rule.Mode == ShardModeOptional && val == "" {
		return candidates, nil
	}

	filtered := make([]*NodeInfo, 0, len(candidates))
	for _, n := range candidates {
		if n.Metadata[rule.MetadataKey] == val {
			filtered = append(filtered, n)
		}
	}

	if len(filtered) == 0 {
		switch rule.EmptyCandidate {
		case EmptyCandidatePolicyError:
			return nil, fmt.Errorf("cluster shard: no candidates for service %q with %s=%s: %w",
				serviceName, rule.PayloadKey, val, ErrEmptyCandidates)
		default:
			return candidates, nil
		}
	}

	return filtered, nil
}
