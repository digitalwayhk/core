package event

type ProductChanged struct {
	Metadata
	SupplierID uint   `json:"supplierID"`
	ProductID  uint   `json:"productID"`
	Action     string `json:"action"`
}
