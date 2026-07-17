package event

type SupplierChanged struct {
	Metadata
	SupplierID uint   `json:"supplierID"`
	Action     string `json:"action"`
}
