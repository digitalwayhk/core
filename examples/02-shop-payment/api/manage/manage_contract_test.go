package manage

import (
	"testing"

	"github.com/digitalwayhk/core/examples/02-shop-payment/models"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPaymentTypeManageExposesControlledStateCommands(t *testing.T) {
	manager := NewPaymentTypeManage()
	model := managepkg.GetViewModel(manager)
	require.NotNil(t, model)

	titles := make(map[string]string)
	for _, command := range model.Commands {
		titles[command.Name] = command.Title
	}
	assert.Equal(t, "启用", titles["EnablePaymentType"])
	assert.Equal(t, "禁用", titles["DisablePaymentType"])

	field := &view.FieldModel{Field: "Enabled"}
	manager.ViewFieldModel(nil, field)
	require.NotNil(t, field.ComVtp)
	assert.Equal(t, "禁用", field.ComVtp.Items[0])
	assert.Equal(t, "启用", field.ComVtp.Items[1])
	assert.False(t, field.IsEdit)
}

func TestPaymentRecordManageFormatsStatusAndUsesBusinessCommands(t *testing.T) {
	manager := NewPaymentRecordManage()
	model := managepkg.GetViewModel(manager)
	require.NotNil(t, model)

	titles := make(map[string]string)
	for _, command := range model.Commands {
		titles[command.Name] = command.Title
	}
	assert.Equal(t, "确认支付", titles["ConfirmPayment"])
	assert.Equal(t, "支付失败", titles["FailPayment"])
	assert.Equal(t, "确认退款", titles["ConfirmRefund"])

	field := &view.FieldModel{Field: "Status"}
	manager.ViewFieldModel(nil, field)
	require.NotNil(t, field.ComVtp)
	assert.Equal(t, models.PaymentStatusPaid.String(), field.ComVtp.Items[int(models.PaymentStatusPaid)])
	assert.Equal(t, models.PaymentStatusRefunded.String(), field.ComVtp.Items[int(models.PaymentStatusRefunded)])
	assert.False(t, field.IsEdit)
}

func TestOrderManageIsReadOnlyAndFormatsBothStates(t *testing.T) {
	manager := NewOrderManage()
	assert.Len(t, manager.Routers(), 2)

	status := &view.FieldModel{Field: "Status"}
	manager.ViewFieldModel(nil, status)
	require.NotNil(t, status.ComVtp)
	assert.Equal(t, "已撤销", status.ComVtp.Items[int(models.OrderStatusCancelled)])

	payment := &view.FieldModel{Field: "PaymentStatus"}
	manager.ViewFieldModel(nil, payment)
	require.NotNil(t, payment.ComVtp)
	assert.Equal(t, "退款中", payment.ComVtp.Items[int(models.PaymentStatusRefunding)])
}
