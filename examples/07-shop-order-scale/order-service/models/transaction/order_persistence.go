// Package transaction 提供 07 订单服务远程权威订单的持久化能力。
package transaction

import (
	"errors"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models/internal/store"
	persistencetypes "github.com/digitalwayhk/core/pkg/persistence/types"
)

// FindRemoteOrderByIdempotencyWith 按 UserID + requestID 查找远程权威订单。
func FindRemoteOrderByIdempotencyWith(action persistencetypes.IDataAction, userID uint, requestID string) (*Order, error) {
	var items []*Order
	query := store.NewSearch(NewOrder(), 1)
	query.AddWhereN("UserID", userID)
	query.AddWhereN("RequestID", strings.TrimSpace(requestID))
	if err := action.Load(query, &items); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, errors.New("远程订单不存在")
	}
	return items[0], nil
}

// UpsertRemoteOrderWith 将订单事实幂等写入远程权威库。
func UpsertRemoteOrderWith(action persistencetypes.IDataAction, order *Order) (*Order, error) {
	if order == nil {
		return nil, errors.New("订单不能为空")
	}
	existing, err := FindRemoteOrderByIdempotencyWith(action, order.UserID, order.RequestID)
	if err == nil && existing != nil {
		if existing.RequestFingerprint != order.RequestFingerprint {
			return nil, errors.New("幂等键已用于不同订单请求")
		}
		return existing, nil
	}
	now := time.Now().UTC()
	order.OrderStatus = OrderStatusSynced
	order.SyncedAt = &now
	if order.AcceptedAt.IsZero() {
		order.AcceptedAt = now
	}
	if err := order.InsertWith(action); err != nil {
		if existing, findErr := FindRemoteOrderByIdempotencyWith(action, order.UserID, order.RequestID); findErr == nil && existing != nil {
			if existing.RequestFingerprint != order.RequestFingerprint {
				return nil, errors.New("幂等键已用于不同订单请求")
			}
			return existing, nil
		}
		return nil, err
	}
	return order, nil
}

// UpsertRemoteOrdersWith 在当前事务中批量查询幂等键，并用一条批量 INSERT 写入缺失订单。
// 返回结果按批次内首次出现的幂等键排序；同键同指纹的重复项只写入一次。
func UpsertRemoteOrdersWith(action persistencetypes.IDataAction, orders []*Order) ([]*Order, error) {
	if action == nil {
		return nil, errors.New("数据操作器不能为空")
	}
	unique := make([]*Order, 0, len(orders))
	byHash := make(map[string]*Order, len(orders))
	hashes := make([]string, 0, len(orders))
	for _, order := range orders {
		if order == nil {
			return nil, errors.New("订单不能为空")
		}
		if err := order.validate(); err != nil {
			return nil, err
		}
		hash := order.GetHash()
		if previous, ok := byHash[hash]; ok {
			if previous.RequestFingerprint != order.RequestFingerprint {
				return nil, errors.New("幂等键已用于不同订单请求")
			}
			continue
		}
		order.SetHashcode(hash)
		byHash[hash] = order
		unique = append(unique, order)
		hashes = append(hashes, hash)
	}
	if len(unique) == 0 {
		return nil, nil
	}

	var existing []*Order
	query := store.NewSearch(NewOrder(), len(hashes))
	query.AddWhereNS("Hashcode", persistencetypes.SymbolIn, hashes)
	if err := action.Load(query, &existing); err != nil {
		return nil, err
	}
	existingByHash := make(map[string]*Order, len(existing))
	for _, order := range existing {
		if order != nil {
			existingByHash[order.GetHash()] = order
		}
	}

	now := time.Now().UTC()
	stored := make([]*Order, 0, len(unique))
	missing := make([]*Order, 0, len(unique))
	for _, order := range unique {
		if current := existingByHash[order.GetHash()]; current != nil {
			if current.RequestFingerprint != order.RequestFingerprint {
				return nil, errors.New("幂等键已用于不同订单请求")
			}
			stored = append(stored, current)
			continue
		}
		order.OrderStatus = OrderStatusSynced
		order.SyncedAt = &now
		if order.AcceptedAt.IsZero() {
			order.AcceptedAt = now
		}
		missing = append(missing, order)
		stored = append(stored, order)
	}
	if len(missing) > 0 {
		if err := action.Insert(missing); err != nil {
			return nil, err
		}
	}
	return stored, nil
}
