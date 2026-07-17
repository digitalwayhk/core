// Package models 保存 Supplier Service 独占的 SQLite 业务事实。
package models

import (
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	eventdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/event"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
)

const databaseName = "shop-supplier"

var (
	actionOnce     sync.Once
	storageOnce    sync.Once
	action         persistencetypes.IDataAction
	actionTemplate persistencetypes.IDataAction
	storageErr     error
	transactionMu  sync.Mutex
)

type Supplier struct {
	*entity.Model
	AuthUserID  string `gorm:"not null;uniqueIndex" json:"-"`
	Name        string `gorm:"not null;uniqueIndex" json:"name"`
	Code        string `gorm:"not null;uniqueIndex" json:"code"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

func NewSupplier() *Supplier { return &Supplier{Model: entity.NewModel()} }
func (s *Supplier) NewModel() {
	if s.Model == nil {
		s.Model = entity.NewModel()
	}
}
func (*Supplier) GetLocalDBName() string  { return databaseName }
func (*Supplier) GetRemoteDBName() string { return databaseName }
func (s *Supplier) GetHash() string {
	return utils.HashCodes(strings.ToLower(strings.TrimSpace(s.AuthUserID)))
}

type Product struct {
	*entity.Model
	SupplierID uint            `gorm:"not null;index:idx_product_supplier_code,unique" json:"supplierID"`
	Name       string          `gorm:"not null" json:"name"`
	Code       string          `gorm:"not null;index:idx_product_supplier_code,unique" json:"code"`
	Price      decimal.Decimal `json:"price"`
	Enabled    bool            `json:"enabled"`
}

func NewProduct() *Product { return &Product{Model: entity.NewModel()} }
func (p *Product) NewModel() {
	if p.Model == nil {
		p.Model = entity.NewModel()
	}
}
func (*Product) GetLocalDBName() string  { return databaseName }
func (*Product) GetRemoteDBName() string { return databaseName }
func (p *Product) GetHash() string {
	return utils.HashCodes(strconv.FormatUint(uint64(p.SupplierID), 10), strings.ToLower(strings.TrimSpace(p.Code)))
}

// Outbox 与商品事实同事务写入，发布成功后才标记完成。
type Outbox struct {
	*entity.Model
	EventID   string `gorm:"not null;uniqueIndex" json:"eventID"`
	EventType string `gorm:"not null;index" json:"eventType"`
	Subject   string `gorm:"not null" json:"subject"`
	Payload   []byte `gorm:"type:blob" json:"-"`
	Published bool   `gorm:"index" json:"published"`
}

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
		action := baseAction()
		for _, model := range []interface{}{NewSupplier(), NewProduct(), NewSupplierOrder(), NewOutbox(), NewInbox()} {
			if err := ensureWith(action, model); err != nil {
				storageErr = err
				return
			}
		}
		if cloner, ok := action.(interface {
			Clone() persistencetypes.IDataAction
		}); ok {
			actionTemplate = cloner.Clone()
		} else {
			actionTemplate = action
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

func (s *Supplier) Save() error {
	s.AuthUserID, s.Name, s.Code = strings.TrimSpace(s.AuthUserID), strings.TrimSpace(s.Name), strings.ToLower(strings.TrimSpace(s.Code))
	if s.AuthUserID == "" || s.Name == "" || s.Code == "" {
		return errors.New("供应商身份、名称和编码不能为空")
	}
	s.SetHashcode(s.GetHash())
	if s.ID == 0 {
		return dataAction().Insert(s)
	}
	s.SetUpdatedAt(time.Now().UTC())
	return dataAction().Update(s)
}

func (s *Supplier) UpdateWith(action persistencetypes.IDataAction) error {
	s.AuthUserID, s.Name, s.Code = strings.TrimSpace(s.AuthUserID), strings.TrimSpace(s.Name), strings.ToLower(strings.TrimSpace(s.Code))
	if s.AuthUserID == "" || s.Name == "" || s.Code == "" {
		return errors.New("供应商身份、名称和编码不能为空")
	}
	s.SetHashcode(s.GetHash())
	s.SetUpdatedAt(time.Now().UTC())
	return action.Update(s)
}

func FindSupplier(authUserID string) (*Supplier, error) {
	if err := ensureWith(dataAction(), NewSupplier()); err != nil {
		return nil, err
	}
	var items []*Supplier
	q := search(NewSupplier(), 1)
	q.AddWhereN("AuthUserID", strings.TrimSpace(authUserID))
	if err := dataAction().Load(q, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

func FindSupplierByID(id uint) (*Supplier, error) {
	if err := ensureWith(dataAction(), NewSupplier()); err != nil {
		return nil, err
	}
	var items []*Supplier
	q := search(NewSupplier(), 1)
	q.AddWhereN("ID", id)
	if err := dataAction().Load(q, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

func ListSuppliers() ([]*Supplier, error) {
	if err := ensureWith(dataAction(), NewSupplier()); err != nil {
		return nil, err
	}
	var items []*Supplier
	err := dataAction().Load(search(NewSupplier(), 1000), &items)
	return items, err
}

func (p *Product) InsertWith(a persistencetypes.IDataAction) error {
	p.Name, p.Code = strings.TrimSpace(p.Name), strings.ToLower(strings.TrimSpace(p.Code))
	if p.Name == "" || p.Code == "" || p.SupplierID == 0 || !p.Price.GreaterThan(decimal.Zero) {
		return errors.New("商品名称、编码、供应商和正数价格不能为空")
	}
	p.SetHashcode(p.GetHash())
	return a.Insert(p)
}

func (p *Product) UpdateWith(a persistencetypes.IDataAction) error {
	p.Name, p.Code = strings.TrimSpace(p.Name), strings.ToLower(strings.TrimSpace(p.Code))
	if p.Name == "" || p.Code == "" || p.SupplierID == 0 || !p.Price.GreaterThan(decimal.Zero) {
		return errors.New("商品名称、编码、供应商和正数价格不能为空")
	}
	p.SetHashcode(p.GetHash())
	p.SetUpdatedAt(time.Now().UTC())
	return a.Update(p)
}

func FindProduct(id uint) (*Product, error) {
	if err := ensureWith(dataAction(), NewProduct()); err != nil {
		return nil, err
	}
	var items []*Product
	q := search(NewProduct(), 1)
	q.AddWhereN("ID", id)
	if err := dataAction().Load(q, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

func ListProducts() ([]*Product, error) {
	if err := ensureWith(dataAction(), NewProduct()); err != nil {
		return nil, err
	}
	var items []*Product
	err := dataAction().Load(search(NewProduct(), 1000), &items)
	return items, err
}

func NewProductOutbox(eventID, eventType, subject string, payload interface{}) (*Outbox, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	item := NewOutbox()
	item.EventID, item.EventType, item.Subject, item.Payload = eventID, eventType, subject, data
	item.SetHashcode(item.GetHash())
	return item, nil
}

func PendingOutbox() ([]*Outbox, error) {
	if err := ensureWith(dataAction(), NewOutbox()); err != nil {
		return nil, err
	}
	var items []*Outbox
	q := search(NewOutbox(), 100)
	q.AddWhereN("Published", false)
	q.AddSortN("ID", true)
	err := dataAction().Load(q, &items)
	return items, err
}

func MarkOutboxPublished(item *Outbox) error {
	item.Published = true
	item.SetUpdatedAt(time.Now().UTC())
	return dataAction().Update(item)
}

var inboxMu sync.Mutex

func ProcessInbox(eventID, eventType string, operation func() error) error {
	inboxMu.Lock()
	defer inboxMu.Unlock()
	if err := ensureWith(dataAction(), NewInbox()); err != nil {
		return err
	}
	var items []*Inbox
	q := search(NewInbox(), 1)
	q.AddWhereN("EventID", eventID)
	if err := dataAction().Load(q, &items); err != nil {
		return err
	}
	if len(items) > 0 {
		return nil
	}
	if err := operation(); err != nil {
		return err
	}
	item := NewInbox()
	item.EventID, item.EventType = eventID, eventType
	item.SetHashcode(item.GetHash())
	return dataAction().Insert(item)
}

func ProductChangedPayload(eventID string, supplierID, productID uint, action string) eventdto.ProductChanged {
	return eventdto.ProductChanged{Metadata: eventdto.Metadata{
		EventID: eventID, SchemaVersion: 1, EventType: "shop.product.changed", OccurredAt: time.Now().UTC(),
		SourceService: "shop-supplier", AggregateID: eventID,
	}, SupplierID: supplierID, ProductID: productID, Action: action}
}

func SupplierChangedPayload(eventID string, supplierID uint, action string) eventdto.SupplierChanged {
	return eventdto.SupplierChanged{Metadata: eventdto.Metadata{
		EventID: eventID, SchemaVersion: 1, EventType: "shop.supplier.changed", OccurredAt: time.Now().UTC(),
		SourceService: "shop-supplier", AggregateID: strconv.FormatUint(uint64(supplierID), 10),
	}, SupplierID: supplierID, Action: action}
}

func EventID(id uint) string { return strconv.FormatUint(uint64(id), 10) }
