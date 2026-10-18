// 本文件定义纯发送器逐请求结果，不包含凭证。
package orderload

// Result 保存一次请求的确认、错误和耗时。
type Result struct {
	FailureKind string  `json:"failure_kind,omitempty"`
	RequestID   string  `json:"request_id"`
	OrderID     uint    `json:"order_id,omitempty"`
	Status      int     `json:"status"`
	Success     bool    `json:"success"`
	Error       string  `json:"error,omitempty"`
	StartedMS   float64 `json:"started_ms"`
	DurationMS  float64 `json:"duration_ms"`
}
