package dto_test

import (
	"encoding/json"
	"testing"
	"time"

	eventdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/event"
	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSharedDTOsUseNumericBusinessIDsAndStableSnapshots(t *testing.T) {
	item := orderdto.Order{
		ID: 11, OrderRevision: 2, UserID: 21, SupplierID: 31, ProductID: 41,
		Product: supplierdto.ProductSnapshot{
			ProductID: 41, SupplierID: 31, SupplierCode: "supplier-31",
			SupplierName: "供应商", ProductCode: "product-41", ProductName: "商品",
			UnitPrice: decimal.RequireFromString("9.90"),
		},
		Address: userdto.AddressSnapshot{
			AddressID: 51, Recipient: "收件人", Phone: "10086", Region: "地区", Detail: "完整地址",
		},
		Quantity: 2, TotalAmount: decimal.RequireFromString("19.80"),
		PaymentStatus: 1, OrderStatus: 2,
	}

	data, err := json.Marshal(item)
	require.NoError(t, err)
	var fields map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &fields))
	assert.Equal(t, float64(21), fields["userID"])
	assert.Equal(t, float64(31), fields["supplierID"])
	assert.Equal(t, float64(41), fields["productID"])
	assert.Equal(t, float64(2), fields["orderRevision"])
	assert.NotContains(t, string(data), "authUserID")
	assert.Contains(t, string(data), `"detail":"完整地址"`)
}

func TestOrderEventContainsRevisionAndFullFulfillmentSnapshot(t *testing.T) {
	now := time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
	payload := eventdto.OrderChanged{
		Metadata: eventdto.Metadata{
			EventID: "event-1", SchemaVersion: 1, EventType: "shop.order.created",
			OccurredAt: now, SourceService: "shop-order", AggregateID: "11",
		},
		OrderRevision: 2, OrderID: 11, UserID: 21, SupplierID: 31, ProductID: 41,
		SupplierCode: "supplier-31", SupplierName: "供应商",
		ProductCode: "product-41", ProductName: "商品",
		UnitPrice: decimal.RequireFromString("9.90"), Quantity: 2,
		TotalAmount: decimal.RequireFromString("19.80"), PaymentStatus: 1, OrderStatus: 2,
		Address: userdto.AddressSnapshot{
			AddressID: 51, Recipient: "收件人", Phone: "10086", Region: "地区", Detail: "完整地址",
		},
		CreatedAt: now, UpdatedAt: now,
	}

	data, err := json.Marshal(payload)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"schemaVersion":1`)
	assert.Contains(t, string(data), `"orderRevision":2`)
	assert.Contains(t, string(data), `"supplierID":31`)
	assert.Contains(t, string(data), `"address":{"addressID":51`)
	assert.Contains(t, string(data), `"detail":"完整地址"`)
	assert.NotContains(t, string(data), "token")
	assert.NotContains(t, string(data), "claims")
	assert.NotContains(t, string(data), "authUserID")
}
