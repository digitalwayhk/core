// Package business 预留 07 订单服务下单引用快照缓存能力。
package business

import "sync"

// OrderReferenceCache 缓存下单所需的供应商和商品最小快照。
type OrderReferenceCache struct {
	mu sync.RWMutex
}

// InvalidateSupplier 失效指定供应商相关引用缓存。
func (c *OrderReferenceCache) InvalidateSupplier(_ uint) {
	c.mu.Lock()
	c.mu.Unlock()
}

// InvalidateProduct 失效指定商品相关引用缓存。
func (c *OrderReferenceCache) InvalidateProduct(_ uint) {
	c.mu.Lock()
	c.mu.Unlock()
}
