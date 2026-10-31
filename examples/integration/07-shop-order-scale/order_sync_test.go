// Package shoporderscale 验证 07 单进程本地 pending 与远程同步异常恢复。
package shoporderscale

import (
	"context"
	"errors"
	"testing"
	"time"

	orderbusiness "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/business"
	ordermodels "github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// TestUATPendingSurvivesRemoteFailure 验证远程失败不会丢失本地可靠订单事实。
func TestUATPendingSurvivesRemoteFailure(t *testing.T) {
	runtime := newIntegrationOrderRuntime(t, failingRemote{})
	unique := uint(time.Now().UnixNano() % 1000000)
	requestID := "pending-failure-uat-" + time.Now().Format("150405.000000000")
	ids := newBenchmarkIDFactory(24)
	_, err := (orderbusiness.LocalOrderWriter{Store: runtime}).Accept(context.Background(), orderbusiness.CreateOrderCommand{
		OrderID:            ids.NewID(),
		UserID:             150000 + unique,
		SupplierID:         250000 + unique,
		ProductID:          350000 + unique,
		RequestID:          requestID,
		RequestFingerprint: requestID,
		UnitPrice:          decimal.NewFromInt(8),
		Quantity:           2,
		TraceID:            "trace-pending-failure",
		ServiceName:        "shop-order",
		ServiceInstanceID:  "order-a",
	})
	require.NoError(t, err)

	syncer := orderbusiness.RemoteOrderSyncer{Store: runtime}
	_, err = syncer.DrainOnce(context.Background(), 10000)
	require.Error(t, err)
	pending, err := runtime.FindLocalByRequest(context.Background(), 150000+unique, requestID)
	require.NoError(t, err)
	require.NotNil(t, pending)
}

type failingRemote struct{}

func (failingRemote) UpsertBatch(_ context.Context, _ []*ordermodels.Order) ([]*ordermodels.Order, error) {
	return nil, errors.New("远程库暂不可用")
}

type commitReplyLostRemote struct{ lost bool }

func (r *commitReplyLostRemote) UpsertBatch(ctx context.Context, orders []*ordermodels.Order) ([]*ordermodels.Order, error) {
	stored, err := (orderbusiness.ModelRemoteOrderStore{}).UpsertBatch(ctx, orders)
	if err != nil {
		return nil, err
	}
	if !r.lost {
		r.lost = true
		return nil, errors.New("测试注入：真实提交已完成，提交回执丢失")
	}
	return stored, nil
}

// TestUATPendingSurvivesCommitReplyLoss 在真实远程提交后注入回执丢失，验证保留pending并幂等汇合。
func TestUATPendingSurvivesCommitReplyLoss(t *testing.T) {
	requireOrderMySQL(t)
	remote := &commitReplyLostRemote{}
	runtime := newIntegrationOrderRuntime(t, remote)
	ids := newBenchmarkIDFactory(25)
	requestID := "commit-unknown-" + time.Now().Format("150405.000000000")
	command := orderbusiness.CreateOrderCommand{OrderID: ids.NewID(), UserID: 180001, SupplierID: 280001, ProductID: 380001, RequestID: requestID, RequestFingerprint: requestID, UnitPrice: decimal.NewFromInt(8), Quantity: 2, TraceID: requestID, ServiceName: "shop-order", ServiceInstanceID: "order-a"}
	_, err := (orderbusiness.LocalOrderWriter{Store: runtime}).Accept(context.Background(), command)
	require.NoError(t, err)
	syncer := orderbusiness.RemoteOrderSyncer{Store: runtime}
	_, err = syncer.DrainOnce(context.Background(), 100)
	require.Error(t, err)
	pending, err := runtime.FindLocalByRequest(context.Background(), command.UserID, requestID)
	require.NoError(t, err)
	require.NotNil(t, pending)
	orders, _, err := orderbusiness.ListOrders(runtime, ordermodels.OrderQueryFilter{UserID: command.UserID}, 1, 20)
	require.NoError(t, err)
	require.Len(t, orders, 1)
	require.Equal(t, command.OrderID, orders[0].ID)
	_, err = syncer.DrainOnce(context.Background(), 100)
	require.NoError(t, err)
	orders, _, err = orderbusiness.ListOrders(runtime, ordermodels.OrderQueryFilter{UserID: command.UserID}, 1, 20)
	require.NoError(t, err)
	require.Len(t, orders, 1)
	require.Equal(t, command.OrderID, orders[0].ID)
}
