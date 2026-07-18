// 本文件定义 06 微服务示例订单域对外传递的 DTO 能力。
package order

// PaymentType 定义本文件能力使用的核心结构。
type PaymentType struct {
	ID      uint   `json:"id"`
	Name    string `json:"name"`
	Code    string `json:"code"`
	Enabled bool   `json:"enabled"`
}
