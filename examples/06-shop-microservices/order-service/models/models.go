// Package models 保存 Order Service 独占的 SQLite 事实。
package models

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"time"

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
	OrderStatusCancelled
)
const (
	PaymentStatusUnpaid = iota
	PaymentStatusProcessing
	PaymentStatusPaid
	PaymentStatusRefunded
)

var (
	actionOnce    sync.Once
	action        persistencetypes.IDataAction
	transactionMu sync.Mutex
)

type Order struct {
	*entity.Model
	IdempotencyKey string          `gorm:"not null;uniqueIndex" json:"idempotencyKey"`
	UserID         string          `gorm:"not null;index" json:"userID"`
	SupplierID     string          `gorm:"not null;index" json:"supplierID"`
	ProductID      uint            `gorm:"not null;index" json:"productID"`
	SupplierName   string          `json:"supplierName"`
	ProductCode    string          `json:"productCode"`
	ProductName    string          `json:"productName"`
	UnitPrice      decimal.Decimal `json:"unitPrice"`
	Quantity       int             `json:"quantity"`
	Recipient      string          `json:"recipient"`
	Phone          string          `json:"phone"`
	Region         string          `json:"region"`
	AddressDetail  string          `json:"addressDetail"`
	AddressID      uint            `json:"addressID"`
	PaymentStatus  int             `json:"paymentStatus"`
	Status         int             `json:"status"`
}

func NewOrder() *Order { return &Order{Model: entity.NewModel()} }
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

func NewPaymentType() *PaymentType { return &PaymentType{Model: entity.NewModel()} }
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
	OrderID uint            `gorm:"not null;index" json:"orderID"`
	Amount  decimal.Decimal `json:"amount"`
	Status  int             `json:"status"`
}

func NewPaymentRecord() *PaymentRecord { return &PaymentRecord{Model: entity.NewModel()} }
func (p *PaymentRecord) NewModel() {
	if p.Model == nil {
		p.Model = entity.NewModel()
	}
}
func (*PaymentRecord) GetLocalDBName() string  { return databaseName }
func (*PaymentRecord) GetRemoteDBName() string { return databaseName }

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

type Inbox struct {
	*entity.Model
	EventID   string `gorm:"not null;uniqueIndex"`
	EventType string `gorm:"not null;index"`
}

func NewInbox() *Inbox { return &Inbox{Model: entity.NewModel()} }
func (i *Inbox) NewModel() {
	if i.Model == nil {
		i.Model = entity.NewModel()
	}
}
func (*Inbox) GetLocalDBName() string  { return databaseName }
func (*Inbox) GetRemoteDBName() string { return databaseName }
func (i *Inbox) GetHash() string       { return utils.HashCodes(i.EventID) }

func dataAction() persistencetypes.IDataAction {
	actionOnce.Do(func() { action = entity.GetGlobalSqliteInstance(databaseName) })
	if cloner, ok := action.(interface {
		Clone() persistencetypes.IDataAction
	}); ok {
		return cloner.Clone()
	}
	return action
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
	for _, m := range []interface{}{NewOrder(), NewPaymentType(), NewPaymentRecord(), NewOutbox(), NewInbox()} {
		if err := ensureWith(dataAction(), m); err != nil {
			return err
		}
	}
	return nil
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

func FindByIdempotency(key string) (*Order, error) {
	if err := ensureWith(dataAction(), NewOrder()); err != nil {
		return nil, err
	}
	var items []*Order
	q := search(NewOrder(), 1)
	q.AddWhereN("IdempotencyKey", strings.TrimSpace(key))
	if err := dataAction().Load(q, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}
func FindOrder(id uint) (*Order, error) {
	if err := ensureWith(dataAction(), NewOrder()); err != nil {
		return nil, err
	}
	var items []*Order
	q := search(NewOrder(), 1)
	q.AddWhereN("ID", id)
	if err := dataAction().Load(q, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}
func ListOrders(field string, value interface{}) ([]*Order, error) {
	if err := ensureWith(dataAction(), NewOrder()); err != nil {
		return nil, err
	}
	var items []*Order
	q := search(NewOrder(), 1000)
	q.AddWhereN(field, value)
	q.AddSortN("ID", false)
	err := dataAction().Load(q, &items)
	return items, err
}
func (o *Order) InsertWith(a persistencetypes.IDataAction) error {
	if strings.TrimSpace(o.IdempotencyKey) == "" || o.UserID == "" || o.SupplierID == "" || o.ProductID == 0 || o.Quantity <= 0 {
		return errors.New("订单参数不完整")
	}
	o.SetHashcode(o.GetHash())
	return a.Insert(o)
}
func (o *Order) UpdateWith(a persistencetypes.IDataAction) error {
	o.SetUpdatedAt(time.Now().UTC())
	return a.Update(o)
}
func (o *Order) DeleteWith(a persistencetypes.IDataAction) error { return a.Delete(o) }

func ListPaymentTypes(enabledOnly bool) ([]*PaymentType, error) {
	if err := ensureWith(dataAction(), NewPaymentType()); err != nil {
		return nil, err
	}
	var items []*PaymentType
	q := search(NewPaymentType(), 100)
	if enabledOnly {
		q.AddWhereN("Enabled", true)
	}
	err := dataAction().Load(q, &items)
	return items, err
}
func SavePaymentType(item *PaymentType) error {
	item.Name = strings.TrimSpace(item.Name)
	item.Code = strings.ToLower(strings.TrimSpace(item.Code))
	if item.Name == "" || item.Code == "" {
		return errors.New("支付类型名称和编码不能为空")
	}
	if item.Hashcode == "" {
		item.SetHashcode(item.GetHash())
		return dataAction().Insert(item)
	}
	item.SetUpdatedAt(time.Now().UTC())
	return dataAction().Update(item)
}
func FindPaymentType(id uint) (*PaymentType, error) {
	if err := ensureWith(dataAction(), NewPaymentType()); err != nil {
		return nil, err
	}
	var items []*PaymentType
	q := search(NewPaymentType(), 1)
	q.AddWhereN("ID", id)
	if err := dataAction().Load(q, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}
func (p *PaymentRecord) InsertWith(a persistencetypes.IDataAction) error { return a.Insert(p) }
func FindPaymentRecord(id uint) (*PaymentRecord, error) {
	if err := ensureWith(dataAction(), NewPaymentRecord()); err != nil {
		return nil, err
	}
	var items []*PaymentRecord
	q := search(NewPaymentRecord(), 1)
	q.AddWhereN("ID", id)
	if err := dataAction().Load(q, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
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
	o := NewOutbox()
	o.EventID, o.EventType, o.Subject, o.Payload = eventID, eventType, subject, data
	o.SetHashcode(o.GetHash())
	return o, nil
}
func PendingOutbox() ([]*Outbox, error) {
	if err := ensureWith(dataAction(), NewOutbox()); err != nil {
		return nil, err
	}
	var items []*Outbox
	q := search(NewOutbox(), 100)
	q.AddWhereN("Published", false)
	err := dataAction().Load(q, &items)
	return items, err
}
func MarkOutboxPublished(o *Outbox) error {
	o.Published = true
	o.SetUpdatedAt(time.Now().UTC())
	return dataAction().Update(o)
}

func ToDTO(o *Order) *orderdto.Order {
	if o == nil {
		return nil
	}
	created := time.Time{}
	if o.CreatedAt != nil {
		created = *o.CreatedAt
	}
	return &orderdto.Order{ID: o.ID, UserID: o.UserID, Product: supplierdto.ProductSnapshot{ProductID: o.ProductID, SupplierID: o.SupplierID, SupplierName: o.SupplierName, ProductCode: o.ProductCode, ProductName: o.ProductName, UnitPrice: o.UnitPrice}, Address: userdto.AddressSnapshot{AddressID: o.AddressID, Recipient: o.Recipient, Phone: o.Phone, Region: o.Region, Detail: o.AddressDetail}, Quantity: o.Quantity, TotalAmount: o.UnitPrice.Mul(decimal.NewFromInt(int64(o.Quantity))), PaymentStatus: o.PaymentStatus, Status: o.Status, CreatedAt: created}
}
func ChangeEvent(eventID, action string, o *Order) eventdto.OrderChanged {
	return eventdto.OrderChanged{Metadata: eventdto.Metadata{EventID: eventID, Version: 1, EventType: "shop.order.changed", OccurredAt: time.Now().UTC(), SourceService: "shop-order", AggregateID: eventID}, UserID: o.UserID, SupplierID: o.SupplierID, OrderID: o.ID, ProductID: o.ProductID, ProductName: o.ProductName, Action: action}
}
