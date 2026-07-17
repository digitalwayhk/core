package event

type PaymentTypeChanged struct {
	Metadata
	PaymentTypeID uint   `json:"paymentTypeID"`
	Action        string `json:"action"`
	Code          string `json:"code"`
	Name          string `json:"name"`
	Enabled       bool   `json:"enabled"`
}
