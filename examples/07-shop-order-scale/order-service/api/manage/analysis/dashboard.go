// Package analysis 提供订单服务标准经营分析看板接口。
// 路径约定：POST /api/manage/{service}/analysis
// 响应：stats.Dashboard（与 admin analysis 页 data.d.ts 对齐）。
package analysis

import (
	"context"
	"strings"
	"time"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/manage/common"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/business"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/zeromicro/go-zero/core/logx"
)

// Dashboard 返回服务经营分析标准模型。
// body: { "refresh": true, "grain": "day" }
type Dashboard struct {
	Refresh bool   `json:"refresh"`
	Grain   string `json:"grain"`
	From    string `json:"from"`
	To      string `json:"to"`
}

// Parse 绑定参数。
func (own *Dashboard) Parse(req servertypes.IRequest) error {
	_ = req.Bind(own)
	return nil
}

// Validation 管理员鉴权。
func (own *Dashboard) Validation(req servertypes.IRequest) error {
	return common.AdminOnly(req)
}

// Do 组装 Dashboard。
// 刷新统计有短超时，失败仍返回当前快照结构（可全 0），保证前端总能拿到 Dashboard 形状。
func (own *Dashboard) Do(req servertypes.IRequest) (interface{}, error) {
	needRefresh := own.Refresh
	if !needRefresh {
		if _, ok := business.OrderStatsStore.Get("order.by_day"); !ok {
			needRefresh = true
		}
	}
	if needRefresh {
		// shop-order 配置 Timeout 仅 3s，刷新必须限时，避免整请求超时导致前端拿不到 body
		ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
		defer cancel()
		done := make(chan struct{})
		go func() {
			defer close(done)
			business.SharedStatsRunner.RefreshNow(ctx)
		}()
		select {
		case <-done:
		case <-ctx.Done():
			logx.Errorw("order_analysis_refresh_timeout",
				logx.Field("error", ctx.Err()),
			)
		}
	}

	dash := business.BuildOrderAnalysisDashboard()
	if g := strings.TrimSpace(own.Grain); g != "" && dash.Query != nil {
		dash.Query.DefaultGrain = g
	}
	// 始终返回非 nil 的 Dashboard 值，避免框架 data 为空
	return dash, nil
}

// RouterInfo 标准 analysis 路由（Manage 域，完整路径含服务名）。
func (own *Dashboard) RouterInfo() *servertypes.RouterInfo {
	return router.NewRouterInfoWithOptions(own,
		"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/manage/analysis",
		"Dashboard",
		router.WithPath("/api/manage/"+contract.OrderServiceName+"/analysis"),
		router.WithAuth(true),
		router.WithPathType(servertypes.ManageType),
		router.WithMethod("POST"),
	)
}

// Reset 对象池复用。
func (own *Dashboard) Reset() {
	*own = Dashboard{}
}
