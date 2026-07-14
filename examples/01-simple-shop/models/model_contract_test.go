package models

import (
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/stretchr/testify/assert"
)

type productPersistence interface {
	Query(id uint, name string) ([]*Product, error)
	FindByID(id uint) (*Product, error)
}

type orderPersistence interface {
	Insert() error
	QueryByUser(userID string) ([]*Order, error)
	FindOwned(id uint, userID string) (*Order, error)
	Delete() error
}

var _ productPersistence = (*Product)(nil)
var _ orderPersistence = (*Order)(nil)

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
