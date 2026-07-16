// Package event 定义跨服务控制事件载荷。
package event

import "time"

type Metadata struct {
	EventID       string    `json:"eventID"`
	Version       int       `json:"version"`
	EventType     string    `json:"eventType"`
	OccurredAt    time.Time `json:"occurredAt"`
	SourceService string    `json:"sourceService"`
	AggregateID   string    `json:"aggregateID"`
}

type ProductChanged struct {
	Metadata
	SupplierID string `json:"supplierID"`
	ProductID  uint   `json:"productID"`
	Action     string `json:"action"`
}

type OrderChanged struct {
	Metadata
	UserID      string `json:"userID"`
	SupplierID  string `json:"supplierID"`
	OrderID     uint   `json:"orderID"`
	ProductID   uint   `json:"productID"`
	ProductName string `json:"productName"`
	Action      string `json:"action"`
}
