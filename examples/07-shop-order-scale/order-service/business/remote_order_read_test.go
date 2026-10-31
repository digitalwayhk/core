// 本文件验证远端幂等查询失败不能被误判为订单不存在。
package business

import (
	"errors"
	"testing"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/stretchr/testify/require"
)

// TestRemoteOrderReadFailure 验证数据库失败向调用方返回，禁止继续接收可能重复的订单。
func TestRemoteOrderReadFailure(t *testing.T) {
	oldRead, oldFind := models.RunRemoteRead, models.FindRemoteOrderByIdempotencyWith
	defer func() { models.RunRemoteRead, models.FindRemoteOrderByIdempotencyWith = oldRead, oldFind }()
	models.RunRemoteRead = func(operation func(models.DataAction) error) error { return operation(nil) }
	want := errors.New("数据库读取失败")
	models.FindRemoteOrderByIdempotencyWith = func(models.DataAction, uint, string) (*models.Order, error) {
		return nil, want
	}
	_, err := findRemoteOrderByRequest(1, "request")
	require.ErrorIs(t, err, want)
	models.FindRemoteOrderByIdempotencyWith = func(models.DataAction, uint, string) (*models.Order, error) {
		return nil, models.ErrRemoteOrderNotFound
	}
	item, err := findRemoteOrderByRequest(1, "missing")
	require.NoError(t, err)
	require.Nil(t, item)
}
