package transaction

const (
	OrderStatusNormal = iota
	OrderStatusCancelling
	OrderStatusCancelled
)

const (
	PaymentStatusUnpaid = iota
	PaymentStatusProcessing
	PaymentStatusPaid
	PaymentStatusFailed
	PaymentStatusRefunding
	PaymentStatusRefunded
)
