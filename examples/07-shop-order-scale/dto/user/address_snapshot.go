// Package user 定义 07 订单水平扩展示例用户域对外传递的 DTO。
package user

// AddressSnapshot 定义订单创建时固化的买家收货地址快照。
type AddressSnapshot struct {
	UserID       uint   `json:"userID"`
	AddressID    uint   `json:"addressID"`
	ReceiverName string `json:"receiverName"`
	Phone        string `json:"phone"`
	Province     string `json:"province"`
	City         string `json:"city"`
	District     string `json:"district"`
	Detail       string `json:"detail"`
	TraceID      string `json:"traceID,omitempty"`
}
