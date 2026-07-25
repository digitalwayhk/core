// Package supplier 定义 07 订单水平扩展示例供应商 DTO。
package supplier

// Supplier 定义跨服务返回的供应商资料快照。
type Supplier struct {
	ID          uint   `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	TraceID     string `json:"traceID"`
}
