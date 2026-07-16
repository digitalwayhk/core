package dto_test

import (
	"encoding/json"
	"testing"

	eventdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/event"
	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSharedDTOsKeepStableJSONFields(t *testing.T) {
	data, err := json.Marshal(orderdto.Order{UserID: "buyer-1"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":0,"userID":"buyer-1","product":{"productID":0,"supplierID":"","supplierName":"","productCode":"","productName":"","unitPrice":"0"},"address":{"addressID":0,"recipient":"","phone":"","region":"","detail":""},"quantity":0,"totalAmount":"0","paymentStatus":0,"paymentID":0,"status":0,"createdAt":"0001-01-01T00:00:00Z"}`, string(data))
}

func TestControlEventDoesNotExposeIdentityCredentials(t *testing.T) {
	data, err := json.Marshal(eventdto.OrderChanged{UserID: "buyer-1", SupplierID: "supplier-1"})
	require.NoError(t, err)
	assert.NotContains(t, string(data), "token")
	assert.NotContains(t, string(data), "claims")
	assert.NotContains(t, string(data), "address")
}
