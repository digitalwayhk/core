// 本文件定义 06 微服务示例事件通道使用的跨服务消息 DTO 能力。
package event

// PaymentTypeChanged 定义本文件能力使用的核心结构。
type PaymentTypeChanged struct {
	Metadata
	PaymentTypeID uint   `json:"paymentTypeID"`
	Action        string `json:"action"`
	Code          string `json:"code"`
	Name          string `json:"name"`
	Enabled       bool   `json:"enabled"`
}
