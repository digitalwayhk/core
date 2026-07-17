package user

type AddressSnapshot struct {
	AddressID uint   `json:"addressID"`
	Recipient string `json:"recipient"`
	Phone     string `json:"phone"`
	Region    string `json:"region"`
	Detail    string `json:"detail"`
}
