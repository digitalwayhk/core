// 本文件定义当前服务交易事实、Outbox、Inbox 或投影模型能力。
package transaction

import (
	"errors"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models/internal/store"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/shopspring/decimal"
)

// InsertWith 实现本类型在当前服务边界中的行为。
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

// ListPaymentRecords 执行本文件能力对应的业务操作。
func ListPaymentRecords(orderID uint) ([]*PaymentRecord, error) {
	return listPaymentRecordsWith(store.Get(), orderID)
}

// ListPaymentRecordsWith 执行本文件能力对应的业务操作。
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

// FindPaymentRecord 执行本文件能力对应的业务操作。
func FindPaymentRecord(id uint) (*PaymentRecord, error) {
	return findPaymentRecordWith(store.Get(), "ID", id)
}

// FindPaymentByPaymentID 执行本文件能力对应的业务操作。
func FindPaymentByPaymentID(paymentID string) (*PaymentRecord, error) {
	return findPaymentRecordWith(store.Get(), "PaymentID", strings.TrimSpace(paymentID))
}

// FindPaymentByPaymentIDWith 执行本文件能力对应的业务操作。
func FindPaymentByPaymentIDWith(action persistencetypes.IDataAction, paymentID string) (*PaymentRecord, error) {
	return findPaymentRecordWith(action, "PaymentID", strings.TrimSpace(paymentID))
}

// UpdateWith 实现本类型在当前服务边界中的行为。
func (p *PaymentRecord) UpdateWith(action persistencetypes.IDataAction) error {
	p.SetUpdatedAt(time.Now().UTC())
	return action.Update(p)
}
