package types

// ExternalRateLimitPolicy 描述单实例外部 Public API 的令牌桶策略。
// Rate 是每秒恢复的令牌数，Burst 是令牌桶容量。
type ExternalRateLimitPolicy struct {
	Rate  float64
	Burst int
}

// Valid 返回策略是否可用于限流。
func (p ExternalRateLimitPolicy) Valid() bool {
	return p.Rate > 0 && p.Burst > 0
}

// ConfigureExternalRateLimit 仅允许在 RouterInfo 注册冻结前设置限流策略。
func (own *RouterInfo) ConfigureExternalRateLimit(policy ExternalRateLimitPolicy) {
	if !policy.Valid() {
		panic("external rate limit policy is invalid")
	}
	own.Lock()
	defer own.Unlock()
	if own.frozen {
		panic("external rate limit policy cannot change after registration")
	}
	own.externalRateLimit = policy
	own.hasExternalRateLimit = true
}

// GetExternalRateLimit 返回冻结限流策略的副本；未配置时返回 nil。
func (own *RouterInfo) GetExternalRateLimit() *ExternalRateLimitPolicy {
	if own == nil {
		return nil
	}
	own.RLock()
	defer own.RUnlock()
	own.assertMetadataFrozenLocked()
	if !own.hasExternalRateLimit {
		return nil
	}
	policy := own.externalRateLimit
	return &policy
}
