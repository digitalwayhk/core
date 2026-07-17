// Package models 保存 Order Service 独占的订单、支付和可靠 Outbox 事实。
package models

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	eventdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/event"
	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	supplierdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/supplier"
	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
)

const databaseName = "shop-order"

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

var (
	actionOnce     sync.Once
	storageOnce    sync.Once
	action         persistencetypes.IDataAction
	actionTemplate persistencetypes.IDataAction
	storageErr     error
	transactionMu  sync.Mutex
)

type Order struct {
	*entity.Model
	IdempotencyKey     string          `gorm:"not null;uniqueIndex" json:"idempotencyKey"`
	RequestFingerprint string          `gorm:"not null" json:"-"`
	OrderRevision      uint64          `gorm:"not null" json:"orderRevision"`
	UserID             uint            `gorm:"not null;index" json:"userID"`
	SupplierID         uint            `gorm:"not null;index" json:"supplierID"`
	ProductID          uint            `gorm:"not null;index" json:"productID"`
	SupplierCode       string          `json:"supplierCode"`
	SupplierName       string          `json:"supplierName"`
	ProductCode        string          `json:"productCode"`
	ProductName        string          `json:"productName"`
	UnitPrice          decimal.Decimal `json:"unitPrice"`
	Quantity           int             `json:"quantity"`
	TotalAmount        decimal.Decimal `json:"totalAmount"`
	Recipient          string          `json:"recipient"`
	Phone              string          `json:"phone"`
	Region             string          `json:"region"`
	AddressDetail      string          `json:"addressDetail"`
	AddressID          uint            `json:"addressID"`
	PaymentStatus      int             `json:"paymentStatus"`
	CurrentPaymentID   string          `gorm:"index" json:"currentPaymentID"`
	OrderStatus        int             `json:"orderStatus"`
}

func NewOrder() *Order {
	return &Order{Model: entity.NewModel(), OrderStatus: OrderStatusNormal, PaymentStatus: PaymentStatusUnpaid}
}
func (o *Order) NewModel() {
	if o.Model == nil {
		o.Model = entity.NewModel()
	}
}
func (*Order) GetLocalDBName() string  { return databaseName }
func (*Order) GetRemoteDBName() string { return databaseName }
func (o *Order) GetHash() string       { return utils.HashCodes(strings.TrimSpace(o.IdempotencyKey)) }

type PaymentType struct {
	*entity.Model
	Name    string `gorm:"not null" json:"name"`
	Code    string `gorm:"not null;uniqueIndex" json:"code"`
	Enabled bool   `json:"enabled"`
}

func NewPaymentType() *PaymentType { return &PaymentType{Model: entity.NewModel(), Enabled: false} }
func (p *PaymentType) NewModel() {
	if p.Model == nil {
		p.Model = entity.NewModel()
	}
}
func (*PaymentType) GetLocalDBName() string  { return databaseName }
func (*PaymentType) GetRemoteDBName() string { return databaseName }
func (p *PaymentType) GetHash() string {
	return utils.HashCodes(strings.ToLower(strings.TrimSpace(p.Code)))
}

type PaymentRecord struct {
	*entity.Model
	OrderID       uint            `gorm:"not null;index" json:"orderID"`
	PaymentTypeID uint            `gorm:"not null;index" json:"paymentTypeID"`
	Attempt       uint            `gorm:"not null;uniqueIndex:idx_payment_attempt" json:"attempt"`
	PaymentID     string          `gorm:"not null;uniqueIndex;uniqueIndex:idx_payment_attempt" json:"paymentID"`
	Amount        decimal.Decimal `json:"amount"`
	Status        int             `json:"status"`
}

func NewPaymentRecord() *PaymentRecord { return &PaymentRecord{Model: entity.NewModel()} }
func (p *PaymentRecord) NewModel() {
	if p.Model == nil {
		p.Model = entity.NewModel()
	}
}
func (*PaymentRecord) GetLocalDBName() string  { return databaseName }
func (*PaymentRecord) GetRemoteDBName() string { return databaseName }
func (p *PaymentRecord) GetHash() string {
	return utils.HashCodes(strconv.FormatUint(uint64(p.OrderID), 10), strconv.FormatUint(uint64(p.Attempt), 10), strings.TrimSpace(p.PaymentID))
}

type Outbox struct {
	*entity.Model
	EventID   string `gorm:"not null;uniqueIndex"`
	EventType string `gorm:"not null;index"`
	Subject   string `gorm:"not null"`
	Payload   []byte `gorm:"type:blob"`
	Published bool   `gorm:"index"`
}

func NewOutbox() *Outbox { return &Outbox{Model: entity.NewModel()} }
func (o *Outbox) NewModel() {
	if o.Model == nil {
		o.Model = entity.NewModel()
	}
}
func (*Outbox) GetLocalDBName() string  { return databaseName }
func (*Outbox) GetRemoteDBName() string { return databaseName }
func (o *Outbox) GetHash() string       { return utils.HashCodes(o.EventID) }

func baseAction() persistencetypes.IDataAction {
	actionOnce.Do(func() { action = entity.GetGlobalSqliteInstance(databaseName) })
	return action
}

func dataAction() persistencetypes.IDataAction {
	_ = EnsureStorage()
	if actionTemplate == nil {
		return baseAction()
	}
	if cloner, ok := actionTemplate.(interface {
		Clone() persistencetypes.IDataAction
	}); ok {
		return cloner.Clone()
	}
	return actionTemplate
}

func search(model interface{}, size int) *persistencetypes.SearchItem {
	return &persistencetypes.SearchItem{Page: 1, Size: size, Model: model}
}

func ensureWith(a persistencetypes.IDataAction, model interface{}) error {
	t := reflect.TypeOf(model)
	if t == nil || t.Kind() != reflect.Ptr {
		return errors.New("模型类型无效")
	}
	return a.Load(search(model, 1), reflect.New(reflect.SliceOf(t)).Interface())
}

func EnsureStorage() error {
	storageOnce.Do(func() {
		a := baseAction()
		for _, model := range []interface{}{NewOrder(), NewPaymentType(), NewPaymentRecord(), NewOutbox()} {
			if err := ensureWith(a, model); err != nil {
				storageErr = err
				return
			}
		}
		if cloner, ok := a.(interface {
			Clone() persistencetypes.IDataAction
		}); ok {
			actionTemplate = cloner.Clone()
		} else {
			actionTemplate = a
		}
	})
	return storageErr
}

func RunTransaction(operation func(persistencetypes.IDataAction) error) (err error) {
	transactionMu.Lock()
	defer transactionMu.Unlock()
	if err = EnsureStorage(); err != nil {
		return err
	}
	cloner, ok := dataAction().(interface {
		Clone() persistencetypes.IDataAction
	})
	if !ok {
		return errors.New("数据操作器不支持独立事务")
	}
	a := cloner.Clone()
	if err = a.Transaction(); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = a.Rollback()
		}
	}()
	if err = operation(a); err != nil {
		return err
	}
	if err = a.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func findOrderWith(a persistencetypes.IDataAction, field string, value interface{}) (*Order, error) {
	var items []*Order
	query := search(NewOrder(), 1)
	query.AddWhereN(field, value)
	if err := a.Load(query, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

func FindByIdempotency(key string) (*Order, error) {
	if err := EnsureStorage(); err != nil {
		return nil, err
	}
	return findOrderWith(dataAction(), "IdempotencyKey", strings.TrimSpace(key))
}

func FindByIdempotencyWith(a persistencetypes.IDataAction, key string) (*Order, error) {
	return findOrderWith(a, "IdempotencyKey", strings.TrimSpace(key))
}

func FindOrder(id uint) (*Order, error) {
	if err := EnsureStorage(); err != nil {
		return nil, err
	}
	return findOrderWith(dataAction(), "ID", id)
}

func FindOrderWith(a persistencetypes.IDataAction, id uint) (*Order, error) {
	return findOrderWith(a, "ID", id)
}

func ListOrders(field string, value interface{}) ([]*Order, error) {
	if err := EnsureStorage(); err != nil {
		return nil, err
	}
	var items []*Order
	query := search(NewOrder(), 1000)
	query.AddWhereN(field, value)
	query.AddSortN("ID", false)
	err := dataAction().Load(query, &items)
	return items, err
}

func (o *Order) InsertWith(a persistencetypes.IDataAction) error {
	if strings.TrimSpace(o.IdempotencyKey) == "" || strings.TrimSpace(o.RequestFingerprint) == "" || o.UserID == 0 || o.SupplierID == 0 || o.ProductID == 0 || o.Quantity <= 0 || o.OrderRevision == 0 {
		return errors.New("订单参数不完整")
	}
	o.SetHashcode(o.GetHash())
	return a.Insert(o)
}

func (o *Order) UpdateWith(a persistencetypes.IDataAction) error {
	o.SetUpdatedAt(time.Now().UTC())
	return a.Update(o)
}

func ListPaymentTypes(enabledOnly bool) ([]*PaymentType, error) {
	if err := EnsureStorage(); err != nil {
		return nil, err
	}
	var items []*PaymentType
	query := search(NewPaymentType(), 100)
	if enabledOnly {
		query.AddWhereN("Enabled", true)
	}
	err := dataAction().Load(query, &items)
	return items, err
}

func FindPaymentType(id uint) (*PaymentType, error) {
	if err := EnsureStorage(); err != nil {
		return nil, err
	}
	var items []*PaymentType
	query := search(NewPaymentType(), 1)
	query.AddWhereN("ID", id)
	if err := dataAction().Load(query, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

func FindPaymentTypeWith(a persistencetypes.IDataAction, id uint) (*PaymentType, error) {
	var items []*PaymentType
	query := search(NewPaymentType(), 1)
	query.AddWhereN("ID", id)
	if err := a.Load(query, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

func PaymentTypeInUse(id uint) (bool, error) {
	if err := EnsureStorage(); err != nil {
		return false, err
	}
	var items []*PaymentRecord
	query := search(NewPaymentRecord(), 1)
	query.AddWhereN("PaymentTypeID", id)
	if err := dataAction().Load(query, &items); err != nil {
		return false, err
	}
	return len(items) > 0, nil
}

func SavePaymentType(item *PaymentType) error {
	item.Name = strings.TrimSpace(item.Name)
	item.Code = strings.ToLower(strings.TrimSpace(item.Code))
	if item.Name == "" || item.Code == "" {
		return errors.New("支付类型名称和编码不能为空")
	}
	if item.ID != 0 {
		if old, err := FindPaymentType(item.ID); err == nil && old != nil && old.Code != item.Code {
			used, useErr := PaymentTypeInUse(item.ID)
			if useErr != nil {
				return useErr
			}
			if used {
				return contract.ErrResourceInUse
			}
		}
	}
	item.SetHashcode(item.GetHash())
	if item.CreatedAt == nil {
		return dataAction().Insert(item)
	}
	item.SetUpdatedAt(time.Now().UTC())
	return dataAction().Update(item)
}

func DeletePaymentType(item *PaymentType) error {
	used, err := PaymentTypeInUse(item.ID)
	if err != nil {
		return err
	}
	if used {
		return contract.ErrResourceInUse
	}
	return dataAction().Delete(item)
}

func (p *PaymentRecord) InsertWith(a persistencetypes.IDataAction) error {
	if p.OrderID == 0 || p.PaymentTypeID == 0 || p.Attempt == 0 || strings.TrimSpace(p.PaymentID) == "" || !p.Amount.GreaterThan(decimal.Zero) {
		return errors.New("支付流水参数不完整")
	}
	p.PaymentID = strings.TrimSpace(p.PaymentID)
	p.SetHashcode(p.GetHash())
	return a.Insert(p)
}

func listPaymentRecordsWith(a persistencetypes.IDataAction, orderID uint) ([]*PaymentRecord, error) {
	var items []*PaymentRecord
	query := search(NewPaymentRecord(), 100)
	query.AddWhereN("OrderID", orderID)
	query.AddSortN("Attempt", false)
	err := a.Load(query, &items)
	return items, err
}

func ListPaymentRecords(orderID uint) ([]*PaymentRecord, error) {
	if err := EnsureStorage(); err != nil {
		return nil, err
	}
	return listPaymentRecordsWith(dataAction(), orderID)
}

func ListPaymentRecordsWith(a persistencetypes.IDataAction, orderID uint) ([]*PaymentRecord, error) {
	return listPaymentRecordsWith(a, orderID)
}

func findPaymentRecordWith(a persistencetypes.IDataAction, field string, value interface{}) (*PaymentRecord, error) {
	var items []*PaymentRecord
	query := search(NewPaymentRecord(), 1)
	query.AddWhereN(field, value)
	if err := a.Load(query, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

func FindPaymentRecord(id uint) (*PaymentRecord, error) {
	if err := EnsureStorage(); err != nil {
		return nil, err
	}
	return findPaymentRecordWith(dataAction(), "ID", id)
}

func FindPaymentByPaymentID(paymentID string) (*PaymentRecord, error) {
	if err := EnsureStorage(); err != nil {
		return nil, err
	}
	return findPaymentRecordWith(dataAction(), "PaymentID", strings.TrimSpace(paymentID))
}

func FindPaymentByPaymentIDWith(a persistencetypes.IDataAction, paymentID string) (*PaymentRecord, error) {
	return findPaymentRecordWith(a, "PaymentID", strings.TrimSpace(paymentID))
}

func (p *PaymentRecord) UpdateWith(a persistencetypes.IDataAction) error {
	p.SetUpdatedAt(time.Now().UTC())
	return a.Update(p)
}

func NewOutboxRecord(eventID, eventType, subject string, payload interface{}) (*Outbox, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	outbox := NewOutbox()
	outbox.EventID, outbox.EventType, outbox.Subject, outbox.Payload = strings.TrimSpace(eventID), eventType, subject, data
	outbox.SetHashcode(outbox.GetHash())
	return outbox, nil
}

func PendingOutbox() ([]*Outbox, error) {
	if err := EnsureStorage(); err != nil {
		return nil, err
	}
	var items []*Outbox
	query := search(NewOutbox(), 100)
	query.AddWhereN("Published", false)
	err := dataAction().Load(query, &items)
	return items, err
}

func MarkOutboxPublished(outbox *Outbox) error {
	outbox.Published = true
	outbox.SetUpdatedAt(time.Now().UTC())
	return dataAction().Update(outbox)
}

func modelTimes(model *entity.Model) (time.Time, time.Time) {
	created, updated := time.Time{}, time.Time{}
	if model != nil && model.CreatedAt != nil {
		created = *model.CreatedAt
	}
	if model != nil && model.UpdatedAt != nil {
		updated = *model.UpdatedAt
	}
	if updated.IsZero() {
		updated = created
	}
	return created, updated
}

func ToDTO(order *Order) *orderdto.Order {
	if order == nil {
		return nil
	}
	created, updated := modelTimes(order.Model)
	return &orderdto.Order{
		ID: order.ID, OrderRevision: order.OrderRevision, UserID: order.UserID, SupplierID: order.SupplierID, ProductID: order.ProductID,
		Product:  supplierdto.ProductSnapshot{ProductID: order.ProductID, SupplierID: order.SupplierID, SupplierCode: order.SupplierCode, SupplierName: order.SupplierName, ProductCode: order.ProductCode, ProductName: order.ProductName, UnitPrice: order.UnitPrice},
		Address:  userdto.AddressSnapshot{AddressID: order.AddressID, Recipient: order.Recipient, Phone: order.Phone, Region: order.Region, Detail: order.AddressDetail},
		Quantity: order.Quantity, TotalAmount: order.TotalAmount, PaymentStatus: order.PaymentStatus, CurrentPayment: order.CurrentPaymentID,
		OrderStatus: order.OrderStatus, CreatedAt: created, UpdatedAt: updated,
	}
}

func PaymentToDTO(record *PaymentRecord) *orderdto.PaymentRecord {
	if record == nil {
		return nil
	}
	created, updated := modelTimes(record.Model)
	return &orderdto.PaymentRecord{ID: record.ID, OrderID: record.OrderID, PaymentTypeID: record.PaymentTypeID, Attempt: record.Attempt, PaymentID: record.PaymentID, Amount: record.Amount, Status: record.Status, CreatedAt: created, UpdatedAt: updated}
}

func ChangeEvent(eventID, eventType, action string, order *Order) eventdto.OrderChanged {
	created, updated := modelTimes(order.Model)
	return eventdto.OrderChanged{
		Metadata:      eventdto.Metadata{EventID: eventID, SchemaVersion: contract.EventSchemaVersion, EventType: eventType, OccurredAt: time.Now().UTC(), SourceService: contract.OrderServiceName, AggregateID: strconv.FormatUint(uint64(order.ID), 10)},
		OrderRevision: order.OrderRevision, OrderID: order.ID, UserID: order.UserID, SupplierID: order.SupplierID, ProductID: order.ProductID,
		SupplierCode: order.SupplierCode, SupplierName: order.SupplierName, ProductCode: order.ProductCode, ProductName: order.ProductName,
		UnitPrice: order.UnitPrice, Quantity: order.Quantity, TotalAmount: order.TotalAmount, PaymentStatus: order.PaymentStatus, OrderStatus: order.OrderStatus,
		Address:   userdto.AddressSnapshot{AddressID: order.AddressID, Recipient: order.Recipient, Phone: order.Phone, Region: order.Region, Detail: order.AddressDetail},
		CreatedAt: created, UpdatedAt: updated, Action: action,
	}
}
