package private

import (
	"testing"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/dto/event"
	"github.com/stretchr/testify/require"
)

func TestAddOrderRequiresClientRequestID(t *testing.T) {
	api := &AddOrder{ProductID: 1, Quantity: 1, AddressID: 1}
	err := api.Validation(nil)
	require.ErrorContains(t, err, "requestID")
}

func TestGetOrdersNoticeOnlyMatchesNumericUserID(t *testing.T) {
	subscription := &GetOrders{subscriptionUserID: 20}
	match, _ := subscription.NoticeFiltersRouter(&event.OrderChanged{UserID: 20}, subscription)
	other, _ := subscription.NoticeFiltersRouter(&event.OrderChanged{UserID: 21}, subscription)
	require.True(t, match)
	require.False(t, other)
}
