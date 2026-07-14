package models

import (
	"errors"
	"testing"
	"time"

	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type productPersistence interface {
	Query(action persistencetypes.IDataAction, id uint, name string) ([]*Product, error)
	FindByID(action persistencetypes.IDataAction, id uint) (*Product, error)
}

type orderPersistence interface {
	Insert(action persistencetypes.IDataAction) error
	QueryByUser(action persistencetypes.IDataAction, userID string) ([]*Order, error)
	FindOwned(action persistencetypes.IDataAction, id uint, userID string) (*Order, error)
	Delete(action persistencetypes.IDataAction) error
}

var _ productPersistence = (*Product)(nil)
var _ orderPersistence = (*Order)(nil)

type recordingDataAction struct {
	loadCalls   int
	insertCalls int
	insertErr   error
}

func (own *recordingDataAction) Transaction() error { return nil }
func (own *recordingDataAction) Load(*persistencetypes.SearchItem, interface{}) error {
	own.loadCalls++
	return nil
}
func (own *recordingDataAction) Insert(interface{}) error {
	own.insertCalls++
	return own.insertErr
}
func (own *recordingDataAction) Update(interface{}) error                    { return nil }
func (own *recordingDataAction) Delete(interface{}) error                    { return nil }
func (own *recordingDataAction) Raw(string, interface{}) error               { return nil }
func (own *recordingDataAction) Exec(string, interface{}) error              { return nil }
func (own *recordingDataAction) GetModelDB(interface{}) (interface{}, error) { return nil, nil }
func (own *recordingDataAction) Commit() error                               { return nil }
func (own *recordingDataAction) GetRunDB() interface{}                       { return nil }
func (own *recordingDataAction) Rollback() error                             { return nil }

// TestProductHashUsesTrimmedName 验证商品哈希只由规范化名称决定。
func TestProductHashUsesTrimmedName(t *testing.T) {
	product := NewProduct()
	product.Name = "  唯一商品  "
	assert.Equal(t, utils.HashCodes("唯一商品"), product.GetHash())
}

// TestOrderHashUsesUserProductAndSecond 验证订单哈希在同一秒内相同，跨秒后改变。
func TestOrderHashUsesUserProductAndSecond(t *testing.T) {
	base := time.Date(2026, 7, 14, 12, 30, 45, 123456789, time.UTC)
	first := NewOrder()
	first.UserID = "user-1"
	first.ProductID = 42
	first.SetCreatedAt(base)

	sameSecond := NewOrder()
	sameSecond.UserID = "user-1"
	sameSecond.ProductID = 42
	sameSecond.SetCreatedAt(base.Add(500 * time.Millisecond))

	nextSecond := NewOrder()
	nextSecond.UserID = "user-1"
	nextSecond.ProductID = 42
	nextSecond.SetCreatedAt(base.Add(time.Second))

	assert.Equal(t, first.GetHash(), sameSecond.GetHash())
	assert.NotEqual(t, first.GetHash(), nextSecond.GetHash())
}

// TestPersistenceMethodsRequireDataAction 验证普通 API 的模型方法必须显式接收数据适配器。
func TestPersistenceMethodsRequireDataAction(t *testing.T) {
	_, err := NewProduct().Query(nil, 0, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IDataAction")

	err = NewOrder().Insert(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IDataAction")
}

// TestPersistenceMethodsUseInjectedDataAction 验证模型调用由外部注入的接口，而非全局 SQLite。
func TestPersistenceMethodsUseInjectedDataAction(t *testing.T) {
	action := &recordingDataAction{}
	_, err := NewProduct().Query(action, 0, "示例")
	require.NoError(t, err)
	assert.Equal(t, 1, action.loadCalls)

	order := NewOrder()
	order.UserID = "user-1"
	order.ProductID = 42
	order.SetCreatedAt(time.Date(2026, 7, 14, 12, 30, 45, 999999999, time.UTC))
	require.NoError(t, order.Insert(action))
	assert.Equal(t, 1, action.insertCalls)
	require.NotNil(t, order.CreatedAt)
	assert.Equal(t, 0, order.CreatedAt.Nanosecond())
}

// TestOrderInsertMapsFixedSecondUniqueConflict 验证固定秒级订单冲突返回稳定的公开业务文案。
func TestOrderInsertMapsFixedSecondUniqueConflict(t *testing.T) {
	action := &recordingDataAction{insertErr: errors.New("UNIQUE constraint failed: orders.hashcode")}
	order := NewOrder()
	order.UserID = "user-1"
	order.ProductID = 42
	order.SetCreatedAt(time.Date(2026, 7, 14, 12, 30, 45, 0, time.UTC))

	err := order.Insert(action)
	require.Error(t, err)
	contract := servertypes.ResolvePublicError(err)
	assert.Contains(t, contract.Message, "每秒只能购买一次")
}
