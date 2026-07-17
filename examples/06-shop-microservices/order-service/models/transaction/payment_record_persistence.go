package transaction

import (
	"errors"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/internal/store"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/shopspring/decimal"
)

func (p *PaymentRecord) InsertWith(action persistencetypes.IDataAction) error {
	if p.OrderID == 0 || p.PaymentTypeID == 0 || p.Attempt == 0 || strings.TrimSpace(p.PaymentID) == "" || !p.Amount.GreaterThan(decimal.Zero) {
		return errors.New("支付流水参数不完整")
	}
	p.PaymentID = strings.TrimSpace(p.PaymentID)
	p.SetHashcode(p.GetHash())
	return action.Insert(p)
}

func listPaymentRecordsWith(action persistencetypes.IDataAction, orderID uint) ([]*PaymentRecord, error) {
	var items []*PaymentRecord
	query := store.NewSearch(NewPaymentRecord(), 100)
	query.AddWhereN("OrderID", orderID)
	query.AddSortN("Attempt", false)
	err := action.Load(query, &items)
	return items, err
}

func ListPaymentRecords(orderID uint) ([]*PaymentRecord, error) {
	return listPaymentRecordsWith(store.Get(), orderID)
}

func ListPaymentRecordsWith(action persistencetypes.IDataAction, orderID uint) ([]*PaymentRecord, error) {
	return listPaymentRecordsWith(action, orderID)
}

func findPaymentRecordWith(action persistencetypes.IDataAction, field string, value interface{}) (*PaymentRecord, error) {
	var items []*PaymentRecord
	query := store.NewSearch(NewPaymentRecord(), 1)
	query.AddWhereN(field, value)
	if err := action.Load(query, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

func FindPaymentRecord(id uint) (*PaymentRecord, error) {
	return findPaymentRecordWith(store.Get(), "ID", id)
}

func FindPaymentByPaymentID(paymentID string) (*PaymentRecord, error) {
	return findPaymentRecordWith(store.Get(), "PaymentID", strings.TrimSpace(paymentID))
}

func FindPaymentByPaymentIDWith(action persistencetypes.IDataAction, paymentID string) (*PaymentRecord, error) {
	return findPaymentRecordWith(action, "PaymentID", strings.TrimSpace(paymentID))
}

func (p *PaymentRecord) UpdateWith(action persistencetypes.IDataAction) error {
	p.SetUpdatedAt(time.Now().UTC())
	return action.Update(p)
}
