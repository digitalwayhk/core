// Package reports 提供服务级报表目录与视图 API（与 analysis Dashboard 分离）。
package reports

import (
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/contract"
	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/manage/common"
	"github.com/digitalwayhk/core/pkg/persistence/entity/stats"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// List 列出本服务全部报表菜单项。
// POST /api/manage/shop-order/reports
type List struct{}

// Parse 无体参数。
func (own *List) Parse(req servertypes.IRequest) error { return nil }

// Validation 管理员。
func (own *List) Validation(req servertypes.IRequest) error {
	return common.AdminOnly(req)
}

// Do 返回报表目录。
func (own *List) Do(req servertypes.IRequest) (interface{}, error) {
	return map[string]interface{}{
		"service": contract.OrderServiceName,
		"items":   stats.ListReportMenus(contract.OrderServiceName),
	}, nil
}

// RouterInfo 注册。
func (own *List) RouterInfo() *servertypes.RouterInfo {
	return router.NewRouterInfoWithOptions(own,
		"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/api/manage/reports",
		"List",
		router.WithPath("/api/manage/"+contract.OrderServiceName+"/reports"),
		router.WithAuth(true),
		router.WithPathType(servertypes.ManageType),
		router.WithMethod("POST"),
	)
}

// Reset 池化。
func (own *List) Reset() { *own = List{} }
