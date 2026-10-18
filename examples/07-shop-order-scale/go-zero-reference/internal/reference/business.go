// 本文件独立实现商品查询、订单规则、幂等、接单及本人订单查询。
package reference

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func (a *application) products(id uint) ([]map[string]interface{}, error) {
	var products []Product
	q := a.db.Model(&Product{}).Where("enabled = ?", true)
	if id != 0 {
		q = q.Where("id = ?", id)
	}
	var count int64
	if e := q.Count(&count).Error; e != nil {
		return nil, e
	}
	if e := q.Order("id DESC").Limit(1000).Find(&products).Error; e != nil {
		return nil, e
	}
	result := make([]map[string]interface{}, 0, len(products))
	for _, p := range products {
		var supplier Supplier
		q := a.db.Model(&Supplier{}).Where("id = ?", p.SupplierID)
		if e := q.Count(&count).Error; e != nil {
			return nil, e
		}
		if e := q.Take(&supplier).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				continue
			}
			return nil, e
		}
		if !supplier.Enabled {
			continue
		}
		result = append(result, map[string]interface{}{"id": p.ID, "supplierID": p.SupplierID, "supplierCode": supplier.Code, "supplierName": supplier.Name, "code": p.Code, "name": p.Name, "price": p.Price, "enabled": p.Enabled, "traceID": p.TraceID})
	}
	return result, nil
}

func (a *application) validateRule(r *OrderRequest) error {
	a.ruleMu.RLock()
	rule := a.rule
	a.ruleMu.RUnlock()
	if rule == nil {
		var loaded OrderRule
		e := a.db.Where("rule_code = ? AND enabled = ?", "default", true).Take(&loaded).Error
		if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if errors.Is(e, gorm.ErrRecordNotFound) {
			rule = &OrderRule{MinQuantity: 1, MaxQuantity: 100, MaxOrderAmount: decimal.NewFromInt(99999), Enabled: true}
		} else {
			rule = &loaded
			a.ruleMu.Lock()
			a.rule = rule
			a.ruleMu.Unlock()
		}
	}
	if !rule.Enabled || r.Quantity < rule.MinQuantity || r.Quantity > rule.MaxQuantity || r.UnitPrice.Mul(decimal.NewFromInt(int64(r.Quantity))).GreaterThan(rule.MaxOrderAmount) {
		return errors.New("订单不满足业务规则")
	}
	return nil
}

func (a *application) create(r *OrderRequest, trace string) (*Order, error) {
	if e := r.validate(); e != nil {
		return nil, e
	}
	if r.UserID == 0 || r.SupplierID == 0 || r.OrderID == 0 || !r.UnitPrice.IsPositive() {
		return nil, errors.New("内部下单参数不完整")
	}
	if e := a.validateRule(r); e != nil {
		return nil, e
	}
	old, e := a.local.find(r.UserID, r.RequestID)
	if e != nil {
		return nil, e
	}
	if old == nil {
		old = &Order{}
		e = a.db.Where("user_id = ? AND request_id = ?", r.UserID, r.RequestID).Take(old).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			old = nil
		} else if e != nil {
			return nil, e
		}
	}
	if old != nil {
		if old.RequestFingerprint != r.RequestID {
			return nil, errors.New("幂等键已用于不同订单请求")
		}
		return old, nil
	}
	now := time.Now().UTC()
	o := &Order{Record: Record{CreatedAt: &now, UpdatedAt: &now, TraceID: trace, ServiceName: "shop-order", ServiceInstanceID: "native-order", ServiceInstanceIP: "127.0.0.1"}, ID: r.OrderID, RequestID: r.RequestID, RequestFingerprint: r.RequestID, UserID: r.UserID, SupplierID: r.SupplierID, ProductID: r.ProductID, SupplierCode: r.SupplierCode, SupplierName: r.SupplierName, ProductCode: r.ProductCode, ProductName: r.ProductName, UnitPrice: r.UnitPrice, Quantity: r.Quantity, TotalAmount: r.UnitPrice.Mul(decimal.NewFromInt(int64(r.Quantity))), Recipient: r.Address.ReceiverName, Phone: r.Address.Phone, Region: r.Address.Province + r.Address.City + r.Address.District, AddressDetail: r.Address.Detail, AddressID: r.Address.AddressID, OrderRevision: 1, OrderStatus: "accepted", PaymentStatus: "unpaid", AcceptedAt: now}
	o.Hashcode = hashCodes(o.RequestID, strconv.FormatUint(uint64(o.UserID), 10))
	return a.local.accept(context.Background(), o)
}

func (a *application) orders(user uint) ([]*Order, error) {
	if a.local.depth.Load() < 0 {
		return nil, errInvalidPendingState
	}
	var remote []*Order
	if e := a.db.Where("user_id = ?", user).Order("id DESC").Limit(100).Find(&remote).Error; e != nil {
		return nil, e
	}
	// 缓存depth不能作为分配大小或物理读取上限；保留固定安全上限且不重算/清零计数。
	local, e := a.local.batch(pendingHardLimit)
	if e != nil {
		return nil, e
	}
	if len(local) == pendingHardLimit {
		return nil, errors.New("本地订单列表达到安全读取上限，等待队列恢复后重试")
	}
	byID := map[uint]*Order{}
	for _, o := range remote {
		byID[o.ID] = o
	}
	for _, o := range local {
		if o.UserID == user {
			byID[o.ID] = o
		}
	}
	result := make([]*Order, 0, len(byID))
	for _, o := range byID {
		result = append(result, o)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID > result[j].ID })
	return result, nil
}

func (a *application) requestID(user uint, request string) (uint, error) {
	key := fmt.Sprintf("%d:%s", user, request)
	if id, ok := a.requestIDs.Load(key); ok {
		return id.(uint), nil
	}
	id, e := a.clock.next()
	if e != nil {
		return 0, e
	}
	actual, _ := a.requestIDs.LoadOrStore(key, id)
	return actual.(uint), nil
}
