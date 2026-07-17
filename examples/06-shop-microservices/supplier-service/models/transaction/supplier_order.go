package transaction

import (
	"errors"
	"strconv"
	"time"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	eventdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/event"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models/basedata"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models/common"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/models/internal/store"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
)

// SupplierOrder 是订单可靠事件在 Supplier Service 内形成的永久只读投影。
type SupplierOrder struct {
	*common.BusinessModel
	OrderID        uint            `gorm:"not null;uniqueIndex"`
	OrderRevision  uint64          `gorm:"not null"`
	SupplierID     uint            `gorm:"not null;index"`
	ProductID      uint            `gorm:"not null;index"`
	SupplierCode   string          `gorm:"not null"`
	SupplierName   string          `gorm:"not null"`
	ProductCode    string          `gorm:"not null"`
	ProductName    string          `gorm:"not null"`
	UnitPrice      decimal.Decimal `gorm:"type:text;not null"`
	Quantity       int             `gorm:"not null"`
	TotalAmount    decimal.Decimal `gorm:"type:text;not null"`
	PaymentStatus  int             `gorm:"not null"`
	OrderStatus    int             `gorm:"not null"`
	AddressID      uint
	Recipient      string
	Phone          string
	Region         string
	AddressDetail  string
	OrderCreatedAt time.Time
	OrderUpdatedAt time.Time
}

func NewSupplierOrder() *SupplierOrder {
	return &SupplierOrder{BusinessModel: common.NewBusinessModel()}
}
func (s *SupplierOrder) NewModel() {
	if s.BusinessModel == nil || s.SupplierServiceModel == nil || s.Model == nil {
		s.BusinessModel = common.NewBusinessModel()
	}
}
func (s *SupplierOrder) GetHash() string {
	return utils.HashCodes(strconv.FormatUint(uint64(s.OrderID), 10))
}

func validateOrderEvent(event eventdto.OrderChanged) error {
	if event.SchemaVersion != contract.EventSchemaVersion {
		return errors.New("订单事件版本不受支持")
	}
	if event.EventID == "" || event.OrderID == 0 || event.UserID == 0 || event.SupplierID == 0 || event.ProductID == 0 {
		return errors.New("订单事件缺少关键业务标识")
	}
	return nil
}

func ApplyOrderEvent(event eventdto.OrderChanged) error {
	if err := validateOrderEvent(event); err != nil {
		return err
	}
	return store.RunInTransaction(ensureTransactionStorage, func(action persistencetypes.IDataAction) error {
		var inboxItems []*Inbox
		inboxQuery := store.NewSearch(NewInbox(), 1)
		inboxQuery.AddWhereN("EventID", event.EventID)
		if err := action.Load(inboxQuery, &inboxItems); err != nil {
			return err
		}
		if len(inboxItems) > 0 {
			return nil
		}

		var orders []*SupplierOrder
		orderQuery := store.NewSearch(NewSupplierOrder(), 1)
		orderQuery.AddWhereN("OrderID", event.OrderID)
		if err := action.Load(orderQuery, &orders); err != nil {
			return err
		}
		if len(orders) == 0 {
			item := NewSupplierOrder()
			applyOrderSnapshot(item, event)
			item.SetHashcode(item.GetHash())
			if err := action.Insert(item); err != nil {
				return err
			}
		} else if event.OrderRevision > orders[0].OrderRevision {
			applyOrderSnapshot(orders[0], event)
			orders[0].SetUpdatedAt(time.Now().UTC())
			if err := action.Update(orders[0]); err != nil {
				return err
			}
		}

		inbox := NewInbox()
		inbox.EventID, inbox.EventType = event.EventID, event.EventType
		inbox.SetHashcode(inbox.GetHash())
		return action.Insert(inbox)
	})
}

func applyOrderSnapshot(item *SupplierOrder, event eventdto.OrderChanged) {
	item.OrderID = event.OrderID
	item.OrderRevision = event.OrderRevision
	item.SupplierID = event.SupplierID
	item.ProductID = event.ProductID
	item.SupplierCode = event.SupplierCode
	item.SupplierName = event.SupplierName
	item.ProductCode = event.ProductCode
	item.ProductName = event.ProductName
	item.UnitPrice = event.UnitPrice
	item.Quantity = event.Quantity
	item.TotalAmount = event.TotalAmount
	item.PaymentStatus = event.PaymentStatus
	item.OrderStatus = event.OrderStatus
	item.AddressID = event.Address.AddressID
	item.Recipient = event.Address.Recipient
	item.Phone = event.Address.Phone
	item.Region = event.Address.Region
	item.AddressDetail = event.Address.Detail
	item.OrderCreatedAt = event.CreatedAt
	item.OrderUpdatedAt = event.UpdatedAt
}

func FindSupplierOrder(orderID uint) (*SupplierOrder, error) {
	if err := ensureTransactionStorage(); err != nil {
		return nil, err
	}
	var items []*SupplierOrder
	query := store.NewSearch(NewSupplierOrder(), 1)
	query.AddWhereN("OrderID", orderID)
	if err := store.Get().Load(query, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("供应商订单不存在")
	}
	return items[0], nil
}

func DeleteProduct(item *basedata.Product) error {
	return store.RunInTransaction(ensureTransactionStorage, func(action persistencetypes.IDataAction) error {
		var references []*SupplierOrder
		query := store.NewSearch(NewSupplierOrder(), 1)
		query.AddWhereN("ProductID", item.ID)
		if err := action.Load(query, &references); err != nil {
			return err
		}
		if len(references) > 0 {
			return contract.ErrResourceInUse
		}
		return action.Delete(item)
	})
}

func DeleteSupplier(item *basedata.Supplier) error {
	return store.RunInTransaction(ensureTransactionStorage, func(action persistencetypes.IDataAction) error {
		var products []*basedata.Product
		productQuery := store.NewSearch(basedata.NewProduct(), 1)
		productQuery.AddWhereN("SupplierID", item.ID)
		if err := action.Load(productQuery, &products); err != nil {
			return err
		}
		var references []*SupplierOrder
		orderQuery := store.NewSearch(NewSupplierOrder(), 1)
		orderQuery.AddWhereN("SupplierID", item.ID)
		if err := action.Load(orderQuery, &references); err != nil {
			return err
		}
		if len(products) > 0 || len(references) > 0 {
			return contract.ErrResourceInUse
		}
		return action.Delete(item)
	})
}

func ensureTransactionStorage() error {
	for _, model := range []interface{}{basedata.NewSupplier(), basedata.NewProduct(), NewSupplierOrder(), NewInbox()} {
		if err := store.EnsureModel(model); err != nil {
			return err
		}
	}
	return nil
}
