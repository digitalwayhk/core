package public

import (
	"errors"
	"net/http"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type rejectingOrderRule struct {
	quantity int
	amount   decimal.Decimal
}

func (rule *rejectingOrderRule) ValidateQuantityAndAmount(quantity int, amount decimal.Decimal) error {
	rule.quantity = quantity
	rule.amount = amount
	return errors.New("订单数量超过最大下单数量")
}

func TestCreateOrderValidatesRuleBeforeWritingPending(t *testing.T) {
	rule := &rejectingOrderRule{}
	api := NewCreateOrder(nil)
	api.ruleValidator = rule
	api.UserID = 12345
	api.SupplierID = 831850579048453
	api.ProductID = 831850452875269
	api.Quantity = 130
	api.RequestID = "string1"
	api.UnitPrice = decimal.NewFromInt(7)

	_, err := api.Do(&router.InitRequest{})

	contract := servertypes.ResolvePublicError(err)
	require.Equal(t, 130, rule.quantity)
	require.True(t, decimal.NewFromInt(910).Equal(rule.amount))
	require.Equal(t, servertypes.ErrorKindBusiness, contract.Kind)
	require.Equal(t, http.StatusUnprocessableEntity, contract.HTTPStatus)
	require.Equal(t, "订单数量超过最大下单数量", contract.Message)
}
