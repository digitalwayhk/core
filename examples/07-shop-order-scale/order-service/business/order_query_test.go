// Package business 验证 07 订单查询合并远程权威事实与本地 pending 的一致性边界。
package business

import (
	"testing"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/stretchr/testify/require"
)

// TestMergeOrdersKeepsRemoteAuthorityForSameOrder 验证 ACK 异步删除窗口内，同 ID 的远程状态不被旧 pending 快照覆盖。
func TestMergeOrdersKeepsRemoteAuthorityForSameOrder(t *testing.T) {
	remote := models.NewOrder()
	remote.ID = 101
	remote.OrderStatus = models.OrderStatusSynced
	stalePending := models.NewOrder()
	stalePending.ID = remote.ID
	stalePending.OrderStatus = models.OrderStatusAccepted
	onlyPending := models.NewOrder()
	onlyPending.ID = 102

	merged := mergeOrders([]*models.Order{remote}, []*models.Order{stalePending, onlyPending})
	require.Len(t, merged, 2)
	require.Same(t, onlyPending, merged[0])
	require.Same(t, remote, merged[1])
}
