package user

type Address struct {
	ID        uint   `json:"id"`
	Recipient string `json:"recipient"`
	Phone     string `json:"phone"`
	Region    string `json:"region"`
	Detail    string `json:"detail"`
}
