// Package user 定义 07 订单水平扩展示例地址 DTO。
package user

// Address 定义普通用户收货地址资料。
type Address struct {
	ID           uint   `json:"id"`
	UserID       uint   `json:"userID"`
	ReceiverName string `json:"receiverName"`
	Phone        string `json:"phone"`
	Province     string `json:"province"`
	City         string `json:"city"`
	District     string `json:"district"`
	Detail       string `json:"detail"`
	TraceID      string `json:"traceID"`
}
