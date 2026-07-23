package business

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/digitalwayhk/core/examples/04-shop-performance/contract"
	"github.com/digitalwayhk/core/examples/04-shop-performance/models"
	"github.com/digitalwayhk/core/pkg/server/event"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/syncx"
)

const orderReferenceInvalidationEvent = "performanceshop.order-reference.invalidate"

// orderReferenceSnapshot 是下单所需的最小不可变事实。
// 它不保存完整持久化模型，避免深层继承对象被共享后意外修改。
type orderReferenceSnapshot struct {
	ProductID    uint
	ProductCode  string
	ProductName  string
	SupplierID   uint
	SupplierCode string
	SupplierName string
	UnitPrice    decimal.Decimal
}

type orderReferenceLoader func(productID uint) (*orderReferenceSnapshot, error)

// orderReferenceCache 在服务内缓存商品和供应商的下单事实。
//
// Get 使用 go-zero syncx.SingleFlight 合并同一 ProductID 的冷加载；Manage
// 修改商品或供应商后，Invalidate 通过 ServiceEventBridge 发布控制事件，
// 由订阅回调统一清理。示例默认只在本服务进程内发布；水平扩展时可把
// 同一控制事件显式配置为外发事件。
type orderReferenceCache struct {
	runtime servertypes.RouteEventRuntime
	loader  orderReferenceLoader
	flight  syncx.SingleFlight

	mu     sync.RWMutex
	values map[uint]*orderReferenceSnapshot
	// generation 每次控制事件失效时递增，阻止失效前开始的迟到加载重新写回旧快照。
	generation uint64
	cancel     func()
}

func newOrderReferenceCache(runtime servertypes.RouteEventRuntime, loader orderReferenceLoader) (*orderReferenceCache, error) {
	if runtime == nil {
		return nil, errors.New("订单事实缓存需要 EventBridge")
	}
	if loader == nil {
		return nil, errors.New("订单事实加载器不能为空")
	}
	cache := &orderReferenceCache{
		runtime: runtime,
		loader:  loader,
		flight:  syncx.NewSingleFlight(),
		values:  make(map[uint]*orderReferenceSnapshot),
	}
	cancel, err := runtime.Subscribe(orderReferenceInvalidationEvent, func(*event.Envelope) {
		cache.clear()
	})
	if err != nil {
		return nil, err
	}
	cache.cancel = cancel
	return cache, nil
}

// Get 返回可直接复制到订单快照的不可变事实。
func (c *orderReferenceCache) Get(productID uint) (*orderReferenceSnapshot, error) {
	if productID == 0 {
		return nil, models.NewValidationError("商品 ID 不能为空")
	}
	c.mu.RLock()
	cached := c.values[productID]
	c.mu.RUnlock()
	if cached != nil {
		return cached, nil
	}

	value, err := c.flight.Do(strconv.FormatUint(uint64(productID), 10), func() (interface{}, error) {
		c.mu.RLock()
		doubleChecked := c.values[productID]
		generation := c.generation
		c.mu.RUnlock()
		if doubleChecked != nil {
			return doubleChecked, nil
		}
		loaded, loadErr := c.loader(productID)
		if loadErr != nil {
			return nil, loadErr
		}
		c.mu.Lock()
		if c.generation == generation {
			c.values[productID] = loaded
		}
		c.mu.Unlock()
		return loaded, nil
	})
	if err != nil {
		return nil, err
	}
	return value.(*orderReferenceSnapshot), nil
}

// Invalidate 同步发布控制事件。Publish 返回时，本节点回调已经执行完成。
func (c *orderReferenceCache) Invalidate(ctx context.Context) error {
	envelope := event.NewEnvelope(contract.ServiceName, orderReferenceInvalidationEvent, nil)
	envelope.ShardKey = contract.ServiceName + ":order-reference"
	return c.runtime.Publish(ctx, event.PublishRequest{
		Class:    event.ControlDelivery,
		External: false,
		Envelope: envelope,
	})
}

func (c *orderReferenceCache) clear() {
	c.mu.Lock()
	c.generation++
	c.values = make(map[uint]*orderReferenceSnapshot)
	c.mu.Unlock()
}

// Close 取消 EventBridge 订阅；ServiceContext 会负责关闭 EventBridge 本身。
func (c *orderReferenceCache) Close() {
	if c != nil && c.cancel != nil {
		c.cancel()
	}
}

var (
	orderReferences   atomic.Pointer[orderReferenceCache]
	orderReferencesMu sync.Mutex
)

// StartOrderReferenceCache 为当前服务实例绑定唯一的事实缓存。
func StartOrderReferenceCache(runtime servertypes.RouteEventRuntime) error {
	cache, err := newOrderReferenceCache(runtime, loadOrderReference)
	if err != nil {
		return err
	}
	orderReferencesMu.Lock()
	old := orderReferences.Swap(cache)
	orderReferencesMu.Unlock()
	if old != nil {
		old.Close()
	}
	return nil
}

// StopOrderReferenceCache 注销服务实例的事实缓存。
func StopOrderReferenceCache() {
	orderReferencesMu.Lock()
	cache := orderReferences.Swap(nil)
	orderReferencesMu.Unlock()
	if cache != nil {
		cache.Close()
	}
}

// InvalidateOrderReferenceCache 由 Manage 成功钩子调用，统一经过 EventBridge 处理。
func InvalidateOrderReferenceCache(ctx context.Context) error {
	cache := orderReferences.Load()
	if cache == nil {
		return nil
	}
	return cache.Invalidate(ctx)
}

func getOrderReference(productID uint) (*orderReferenceSnapshot, error) {
	if cache := orderReferences.Load(); cache != nil {
		return cache.Get(productID)
	}
	return loadOrderReference(productID)
}

func loadOrderReference(productID uint) (*orderReferenceSnapshot, error) {
	product, err := models.NewProduct().FindByID(productID)
	if err != nil {
		return nil, err
	}
	if product == nil {
		return nil, models.NewBusinessError("商品不存在")
	}
	if !product.Enabled {
		return nil, models.NewBusinessError("商品已禁用")
	}
	supplier, err := models.NewSupplier().FindByID(product.SupplierID)
	if err != nil {
		return nil, err
	}
	if supplier == nil {
		return nil, models.NewBusinessError("供应商不存在")
	}
	if !supplier.Enabled {
		return nil, models.NewBusinessError("供应商已禁用")
	}
	return &orderReferenceSnapshot{
		ProductID:    product.ID,
		ProductCode:  product.Code,
		ProductName:  product.Name,
		SupplierID:   supplier.ID,
		SupplierCode: supplier.Code,
		SupplierName: supplier.Name,
		UnitPrice:    product.Price,
	}, nil
}
