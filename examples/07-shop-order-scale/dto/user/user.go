// Package user 定义 07 订单水平扩展示例用户 DTO。
package user

// User 定义普通用户资料快照。
type User struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	Enabled bool   `json:"enabled"`
	TraceID string `json:"traceID"`
}
