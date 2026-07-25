// Package order 定义 07 订单水平扩展示例支付类型 DTO。
package order

// PaymentType 定义跨服务返回的支付类型快照。
type PaymentType struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	Code    string `json:"code"`
	Enabled bool   `json:"enabled"`
	TraceID string `json:"traceID"`
}
