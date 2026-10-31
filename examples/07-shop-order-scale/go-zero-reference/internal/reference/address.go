// 本文件声明与 07 相同的收货地址快照。
package reference

// Address 保存下单时的地址事实。
type Address struct {
	UserID       uint   `json:"userID"`
	AddressID    uint   `json:"addressID"`
	ReceiverName string `json:"receiverName"`
	Phone        string `json:"phone"`
	Province     string `json:"province"`
	City         string `json:"city"`
	District     string `json:"district"`
	Detail       string `json:"detail"`
}
