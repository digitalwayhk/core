// 本文件定义当前服务基础资料模型及其持久化能力。
package basedata

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	eventdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/event"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models/internal/store"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/shopspring/decimal"
)

// Save 实现本类型在当前服务边界中的行为。
func (s *Supplier) Save() error {
	s.AuthUserID, s.Name, s.Code = strings.TrimSpace(s.AuthUserID), strings.TrimSpace(s.Name), strings.ToLower(strings.TrimSpace(s.Code))
	if s.AuthUserID == "" || s.Name == "" || s.Code == "" {
		return errors.New("供应商身份、名称和编码不能为空")
	}
	s.SetHashcode(s.GetHash())
	if s.ID == 0 {
		return store.Get().Insert(s)
	}
	s.SetUpdatedAt(time.Now().UTC())
	return store.Get().Update(s)
}

// UpdateWith 实现本类型在当前服务边界中的行为。
func (s *Supplier) UpdateWith(action persistencetypes.IDataAction) error {
	s.AuthUserID, s.Name, s.Code = strings.TrimSpace(s.AuthUserID), strings.TrimSpace(s.Name), strings.ToLower(strings.TrimSpace(s.Code))
	if s.AuthUserID == "" || s.Name == "" || s.Code == "" {
		return errors.New("供应商身份、名称和编码不能为空")
	}
	s.SetHashcode(s.GetHash())
	s.SetUpdatedAt(time.Now().UTC())
	return action.Update(s)
}

// FindSupplier 执行本文件能力对应的业务操作。
func FindSupplier(authUserID string) (*Supplier, error) {
	if err := store.EnsureModel(NewSupplier()); err != nil {
		return nil, err
	}
	var items []*Supplier
	q := store.NewSearch(NewSupplier(), 1)
	q.AddWhereN("AuthUserID", strings.TrimSpace(authUserID))
	if err := store.Get().Load(q, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

// FindSupplierByID 执行本文件能力对应的业务操作。
func FindSupplierByID(id uint) (*Supplier, error) {
	if err := store.EnsureModel(NewSupplier()); err != nil {
		return nil, err
	}
	var items []*Supplier
	q := store.NewSearch(NewSupplier(), 1)
	q.AddWhereN("ID", id)
	if err := store.Get().Load(q, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

// ListSuppliers 执行本文件能力对应的业务操作。
func ListSuppliers() ([]*Supplier, error) {
	if err := store.EnsureModel(NewSupplier()); err != nil {
		return nil, err
	}
	var items []*Supplier
	err := store.Get().Load(store.NewSearch(NewSupplier(), 1000), &items)
	return items, err
}

// InsertWith 实现本类型在当前服务边界中的行为。
func (p *Product) InsertWith(action persistencetypes.IDataAction) error {
	p.Name, p.Code = strings.TrimSpace(p.Name), strings.ToLower(strings.TrimSpace(p.Code))
	if p.Name == "" || p.Code == "" || p.SupplierID == 0 || !p.Price.GreaterThan(decimal.Zero) {
		return errors.New("商品名称、编码、供应商和正数价格不能为空")
	}
	p.SetHashcode(p.GetHash())
	return action.Insert(p)
}

// UpdateWith 实现本类型在当前服务边界中的行为。
func (p *Product) UpdateWith(action persistencetypes.IDataAction) error {
	p.Name, p.Code = strings.TrimSpace(p.Name), strings.ToLower(strings.TrimSpace(p.Code))
	if p.Name == "" || p.Code == "" || p.SupplierID == 0 || !p.Price.GreaterThan(decimal.Zero) {
		return errors.New("商品名称、编码、供应商和正数价格不能为空")
	}
	p.SetHashcode(p.GetHash())
	p.SetUpdatedAt(time.Now().UTC())
	return action.Update(p)
}

// FindProduct 执行本文件能力对应的业务操作。
func FindProduct(id uint) (*Product, error) {
	if err := store.EnsureModel(NewProduct()); err != nil {
		return nil, err
	}
	var items []*Product
	q := store.NewSearch(NewProduct(), 1)
	q.AddWhereN("ID", id)
	if err := store.Get().Load(q, &items); err != nil || len(items) == 0 {
		return nil, err
	}
	return items[0], nil
}

// ListProducts 执行本文件能力对应的业务操作。
func ListProducts() ([]*Product, error) {
	if err := store.EnsureModel(NewProduct()); err != nil {
		return nil, err
	}
	var items []*Product
	err := store.Get().Load(store.NewSearch(NewProduct(), 1000), &items)
	return items, err
}

// ProductChangedPayload 执行本文件能力对应的业务操作。
func ProductChangedPayload(traceID, eventID string, supplierID, productID uint, action string) eventdto.ProductChanged {
	return eventdto.ProductChanged{Metadata: eventdto.Metadata{
		EventID: eventID, TraceID: traceID, SchemaVersion: contract.EventSchemaVersion, EventType: contract.EventProductChanged, OccurredAt: time.Now().UTC(),
		SourceService: contract.SupplierServiceName, AggregateID: strconv.FormatUint(uint64(productID), 10),
	}, SupplierID: supplierID, ProductID: productID, Action: action}
}

// SupplierChangedPayload 执行本文件能力对应的业务操作。
func SupplierChangedPayload(traceID, eventID string, supplierID uint, action string) eventdto.SupplierChanged {
	return eventdto.SupplierChanged{Metadata: eventdto.Metadata{
		EventID: eventID, TraceID: traceID, SchemaVersion: contract.EventSchemaVersion, EventType: contract.EventSupplierChanged, OccurredAt: time.Now().UTC(),
		SourceService: contract.SupplierServiceName, AggregateID: strconv.FormatUint(uint64(supplierID), 10),
	}, SupplierID: supplierID, Action: action}
}

// EventID 执行本文件能力对应的业务操作。
func EventID(id uint) string { return strconv.FormatUint(uint64(id), 10) }
