package models

import (
	"os"
	"testing"
	"time"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	eventdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/event"
	userdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/user"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "supplier-models-")
	if err != nil {
		panic(err)
	}
	utils.TESTPATH = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func orderEvent(eventID string, revision uint64, orderID, supplierID, productID uint) eventdto.OrderChanged {
	now := time.Now().UTC()
	return eventdto.OrderChanged{
		Metadata: eventdto.Metadata{
			EventID: eventID, SchemaVersion: contract.EventSchemaVersion,
			EventType: contract.EventOrderCreated, SourceService: contract.OrderServiceName,
			AggregateID: "order", TraceID: "trace-" + eventID, OccurredAt: now,
		},
		OrderRevision: revision, OrderID: orderID, UserID: 9,
		SupplierID: supplierID, ProductID: productID,
		SupplierCode: "supplier", SupplierName: "供应商",
		ProductCode: "product", ProductName: "商品",
		UnitPrice: decimal.NewFromInt(10), Quantity: 2, TotalAmount: decimal.NewFromInt(20),
		PaymentStatus: 1, OrderStatus: 2,
		Address:   userdto.AddressSnapshot{AddressID: 8, Recipient: "收件人", Phone: "10086", Region: "地区", Detail: "完整地址"},
		CreatedAt: now, UpdatedAt: now,
	}
}

func insertSupplierAndProduct(t *testing.T, supplierID, productID uint) (*Supplier, *Product) {
	t.Helper()
	supplier := NewSupplier()
	supplier.SetID(supplierID)
	supplier.AuthUserID = "auth-supplier"
	supplier.Code = "supplier"
	supplier.Name = "供应商"
	supplier.Enabled = true
	require.NoError(t, supplier.Save())

	product := NewProduct()
	product.SetID(productID)
	product.SupplierID = supplierID
	product.Code = "product"
	product.Name = "商品"
	product.Price = decimal.NewFromInt(10)
	require.NoError(t, RunTransaction(func(action persistencetypes.IDataAction) error {
		return product.InsertWith(action)
	}))
	return supplier, product
}

func TestApplyOrderEventIsIdempotentAndRevisionMonotonic(t *testing.T) {
	supplier, product := insertSupplierAndProduct(t, 100, 200)
	created := orderEvent("event-created", 1, 300, supplier.ID, product.ID)
	require.NoError(t, ApplyOrderEvent(created))
	require.NoError(t, ApplyOrderEvent(created))

	older := orderEvent("event-older", 0, 300, supplier.ID, product.ID)
	older.Address.Detail = "旧地址"
	require.NoError(t, ApplyOrderEvent(older))

	stored, err := FindSupplierOrder(300)
	require.NoError(t, err)
	require.Equal(t, uint64(1), stored.OrderRevision)
	require.Equal(t, "完整地址", stored.AddressDetail)
	require.Equal(t, created.TraceID, stored.TraceID)
	var inboxItems []*Inbox
	query := search(NewInbox(), 10)
	query.AddWhereN("EventID", "event-created")
	require.NoError(t, dataAction().Load(query, &inboxItems))
	require.Len(t, inboxItems, 1)
	require.Equal(t, created.TraceID, inboxItems[0].TraceID)
}

func TestUsedProductAndSupplierCannotBeDeleted(t *testing.T) {
	supplier, product := insertSupplierAndProduct(t, 101, 201)
	require.NoError(t, ApplyOrderEvent(orderEvent("event-used", 1, 301, supplier.ID, product.ID)))

	require.ErrorIs(t, DeleteProduct(product), contract.ErrResourceInUse)
	require.ErrorIs(t, DeleteSupplier(supplier), contract.ErrResourceInUse)
}
