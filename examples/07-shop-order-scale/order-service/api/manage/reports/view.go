package reports

import (
	"context"
	"errors"
	"strings"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/manage/common"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/business"
	"github.com/digitalwayhk/core/pkg/persistence/entity/stats"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// View 获取单报表数据。
// POST /api/manage/shop-order/reports/view
// body: { "code": "order-daily", "refresh": true }
type View struct {
	Code    string `json:"code"`
	Refresh bool   `json:"refresh"`
}

// Parse 绑定。
func (own *View) Parse(req servertypes.IRequest) error {
	_ = req.Bind(own)
	return nil
}

// Validation 管理员。
func (own *View) Validation(req servertypes.IRequest) error {
	return common.AdminOnly(req)
}

// Do 组装报表视图；可选刷新快照。
func (own *View) Do(req servertypes.IRequest) (interface{}, error) {
	code := strings.TrimSpace(own.Code)
	if code == "" {
		return nil, servertypes.NewPublicError(
			servertypes.ErrorKindValidation,
			servertypes.PublicCodeValidation,
			"code 不能为空",
			errors.New("code empty"),
		)
	}
	if own.Refresh {
		// 轻量刷新（与 analysis 一致，有超时保护在 analysis 侧；此处直接触发）
		business.SharedStatsRunner.RefreshNow(context.Background())
	}
	view, err := stats.BuildReportView(business.OrderStatsStore, contract.OrderServiceName, code)
	if err != nil {
		return nil, err
	}
	return view, nil
}

// RouterInfo 注册。
func (own *View) RouterInfo() *servertypes.RouterInfo {
	return router.NewRouterInfoWithOptions(own,
		"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/manage/reports",
		"View",
		router.WithPath("/api/manage/"+contract.OrderServiceName+"/reports/view"),
		router.WithAuth(true),
		router.WithPathType(servertypes.ManageType),
		router.WithMethod("POST"),
	)
}

// Reset 池化。
func (own *View) Reset() { *own = View{} }
