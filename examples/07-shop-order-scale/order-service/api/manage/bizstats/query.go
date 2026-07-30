// Package bizstats 提供订单业务统计只读 Manage API（只读 stats.Store，不查 Model）。
package bizstats

import (
	"context"
	"strings"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/manage/common"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/business"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// Query 读取已刷新的业务统计快照。
// POST /api/manage/shop-order/bizstats/query
// body: { "code": "order.by_day_product" }；code 空则返回全部 order.* 快照列表。
// refresh=true 时先经 Job 路径重算再读 Store（API 仍不直接查业务表做响应拼装）。
type Query struct {
	Code    string `json:"code"`
	Refresh bool   `json:"refresh"`
}

// Parse 绑定查询参数。
func (own *Query) Parse(req servertypes.IRequest) error {
	return req.Bind(own)
}

// Validation 管理员校验。
func (own *Query) Validation(req servertypes.IRequest) error {
	return common.AdminOnly(req)
}

// Do 只读 Store；可选触发 runner 刷新。
func (own *Query) Do(req servertypes.IRequest) (interface{}, error) {
	if own.Refresh {
		business.SharedStatsRunner.RefreshNow(context.Background())
	}
	code := strings.TrimSpace(own.Code)
	if code == "" {
		return map[string]interface{}{
			"items": business.OrderStatsStore.List(),
		}, nil
	}
	snap, ok := business.OrderStatsStore.Get(code)
	if !ok {
		return map[string]interface{}{
			"code":    code,
			"ready":   false,
			"message": "统计尚未就绪，请稍后或传 refresh=true",
		}, nil
	}
	return map[string]interface{}{
		"ready":    true,
		"snapshot": snap,
	}, nil
}

// RouterInfo 注册 Manage 路由。
func (own *Query) RouterInfo() *servertypes.RouterInfo {
	return router.NewRouterInfoWithOptions(own,
		"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/manage/bizstats",
		"Query",
		router.WithPath("/api/manage/"+contract.OrderServiceName+"/bizstats/query"),
		router.WithAuth(true),
		router.WithPathType(servertypes.ManageType),
		router.WithMethod("POST"),
	)
}
