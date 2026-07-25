// Package transaction 验证 07 订单与 Outbox 在单个远程事务内的批量持久化形状。
package transaction

import (
	"errors"
	"testing"

	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type recordingBatchAction struct {
	existingOrders []*Order
	existingOutbox []*OutboxRecord
	orderLoads     int
	outboxLoads    int
	orderInserts   int
	outboxInserts  int
	insertedOrders []*Order
	insertedOutbox []*OutboxRecord
	insertErr      error
}

func (action *recordingBatchAction) Transaction() error { return nil }

func (action *recordingBatchAction) Load(item *persistencetypes.SearchItem, result interface{}) error {
	switch item.Model.(type) {
	case *Order:
		action.orderLoads++
		*result.(*[]*Order) = append([]*Order(nil), action.existingOrders...)
	case *OutboxRecord:
		action.outboxLoads++
		*result.(*[]*OutboxRecord) = append([]*OutboxRecord(nil), action.existingOutbox...)
	default:
		return errors.New("未知模型")
	}
	return nil
}

func (action *recordingBatchAction) Insert(data interface{}) error {
	if action.insertErr != nil {
		return action.insertErr
	}
	switch items := data.(type) {
	case []*Order:
		action.orderInserts++
		action.insertedOrders = append(action.insertedOrders, items...)
	case []*OutboxRecord:
		action.outboxInserts++
		action.insertedOutbox = append(action.insertedOutbox, items...)
	default:
		return errors.New("未知批量写入类型")
	}
	return nil
}

func (action *recordingBatchAction) Update(interface{}) error                    { return nil }
func (action *recordingBatchAction) Delete(interface{}) error                    { return nil }
func (action *recordingBatchAction) Raw(string, interface{}) error               { return nil }
func (action *recordingBatchAction) Exec(string, interface{}) error              { return nil }
func (action *recordingBatchAction) GetModelDB(interface{}) (interface{}, error) { return nil, nil }
func (action *recordingBatchAction) Commit() error                               { return nil }
func (action *recordingBatchAction) GetRunDB() interface{}                       { return nil }
func (action *recordingBatchAction) Rollback() error                             { return nil }

// TestUpsertRemoteOrdersWithUsesOneLoadAndOneInsert 验证多订单只产生一次批量查询和一次批量写入。
func TestUpsertRemoteOrdersWithUsesOneLoadAndOneInsert(t *testing.T) {
	action := &recordingBatchAction{}
	first := newBatchTestOrder(1, 101, "request-1", "fingerprint-1")
	second := newBatchTestOrder(2, 102, "request-2", "fingerprint-2")

	stored, err := UpsertRemoteOrdersWith(action, []*Order{first, second})
	require.NoError(t, err)
	require.Equal(t, []*Order{first, second}, stored)
	require.Equal(t, 1, action.orderLoads)
	require.Equal(t, 1, action.orderInserts)
	require.Equal(t, []*Order{first, second}, action.insertedOrders)
	require.Equal(t, OrderStatusSynced, first.OrderStatus)
	require.NotNil(t, first.SyncedAt)
}

// TestUpsertRemoteOrdersWithDeduplicatesAndRejectsConflict 验证批内同幂等键只写一次，不同指纹则整批拒绝。
func TestUpsertRemoteOrdersWithDeduplicatesAndRejectsConflict(t *testing.T) {
	first := newBatchTestOrder(1, 101, "request-same", "fingerprint-a")
	retry := newBatchTestOrder(2, 101, "request-same", "fingerprint-a")
	action := &recordingBatchAction{}

	stored, err := UpsertRemoteOrdersWith(action, []*Order{first, retry})
	require.NoError(t, err)
	require.Equal(t, []*Order{first}, stored)
	require.Len(t, action.insertedOrders, 1)

	conflict := newBatchTestOrder(3, 101, "request-same", "fingerprint-b")
	_, err = UpsertRemoteOrdersWith(&recordingBatchAction{}, []*Order{first, conflict})
	require.EqualError(t, err, "幂等键已用于不同订单请求")
}

// TestInsertOutboxesIfMissingWithUsesOneLoadAndOneInsert 验证 Outbox 也按已有 EventID 过滤后批量写入。
func TestInsertOutboxesIfMissingWithUsesOneLoadAndOneInsert(t *testing.T) {
	existing := newBatchTestOutbox("event-1")
	existing.SetHashcode(existing.GetHash())
	missing := newBatchTestOutbox("event-2")
	action := &recordingBatchAction{existingOutbox: []*OutboxRecord{existing}}

	err := InsertOutboxesIfMissingWith(action, []*OutboxRecord{existing, missing})
	require.NoError(t, err)
	require.Equal(t, 1, action.outboxLoads)
	require.Equal(t, 1, action.outboxInserts)
	require.Equal(t, []*OutboxRecord{missing}, action.insertedOutbox)
}

func newBatchTestOrder(id, userID uint, requestID, fingerprint string) *Order {
	order := NewOrder()
	order.ID = id
	order.UserID = userID
	order.SupplierID = 201
	order.ProductID = 301
	order.RequestID = requestID
	order.RequestFingerprint = fingerprint
	order.Quantity = 2
	order.UnitPrice = decimal.NewFromInt(10)
	order.TotalAmount = decimal.NewFromInt(20)
	return order
}

func newBatchTestOutbox(eventID string) *OutboxRecord {
	outbox := NewOutbox()
	outbox.EventID = eventID
	outbox.EventType = "order.created"
	outbox.Subject = "shop.order.changed"
	outbox.Payload = []byte(`{"eventID":"` + eventID + `"}`)
	return outbox
}
