// Package user 保存 User Service 对外与跨服务传输结构。
package user

type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Address struct {
	ID        uint   `json:"id"`
	Recipient string `json:"recipient"`
	Phone     string `json:"phone"`
	Region    string `json:"region"`
	Detail    string `json:"detail"`
}

// AddressSnapshot 是下单时写入订单的不可变地址快照。
type AddressSnapshot struct {
	AddressID uint   `json:"addressID"`
	Recipient string `json:"recipient"`
	Phone     string `json:"phone"`
	Region    string `json:"region"`
	Detail    string `json:"detail"`
}
