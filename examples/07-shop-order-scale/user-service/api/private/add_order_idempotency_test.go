// Package private 验证 07 用户入口下单 requestID 到 OrderID 的稳定映射。
package private

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestOrderIDForRequestIsStable 验证同一买家同一 requestID 重试时复用同一订单 ID。
func TestOrderIDForRequestIsStable(t *testing.T) {
	req := &orderIDTestRequest{next: 100}
	first := orderIDForRequest(1, "request-a", req)
	second := orderIDForRequest(1, "request-a", req)
	third := orderIDForRequest(1, "request-b", req)
	require.Equal(t, first, second)
	require.NotEqual(t, first, third)
}

type orderIDTestRequest struct {
	next uint
}

func (r *orderIDTestRequest) NewID() uint {
	r.next++
	return r.next
}
