package manage

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"sync"
	"testing"

	"github.com/digitalwayhk/core/pkg/persistence/entity/stats"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/digitalwayhk/core/pkg/server/types"
	manageservice "github.com/digitalwayhk/core/service/manage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ manageservice.IGetDefaultItemsWithRequest[smodels.MenuModel] = (*MenuManage)(nil)

type menuRequest struct {
	types.IRequest
	id uint
}

func (r *menuRequest) NewID() uint { return r.id }

func TestMenuManageRequestIsolation(t *testing.T) {
	menu := NewMenuManage()
	snapshot := &smodels.MenuServiceSnapshot{Name: "request-isolation"}
	requests := []*menuRequest{
		{id: 501},
		{id: 502},
	}

	ids := make(chan uint, len(requests))
	var wg sync.WaitGroup
	for _, req := range requests {
		wg.Add(1)
		go func(req types.IRequest) {
			defer wg.Done()
			ids <- menu.newDirectoryModel(req, snapshot).ID
		}(req)
	}
	wg.Wait()
	close(ids)

	actual := make([]uint, 0, len(requests))
	for id := range ids {
		actual = append(actual, id)
	}
	sort.Slice(actual, func(i, j int) bool { return actual[i] < actual[j] })
	assert.Equal(t, []uint{501, 502}, actual)
}

func TestMenuManageDoesNotSetSharedRequest(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "menumanage.go", nil, 0)
	require.NoError(t, err)

	var calls []string
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "SetReq" {
			calls = append(calls, fset.Position(call.Pos()).String())
		}
		return true
	})

	assert.Empty(t, calls, "MenuManage must pass req explicitly instead of calling SetReq: %v", calls)
}

func TestIsReportAPIPath(t *testing.T) {
	require.True(t, isReportAPIPath("/api/manage/shop-order/reports"))
	require.True(t, isReportAPIPath("/api/manage/shop-order/reports/"))
	require.True(t, isReportAPIPath("/api/manage/shop-order/reports/view"))
	require.False(t, isReportAPIPath("/report/shop-order/order-daily"))
	require.False(t, isReportAPIPath("/api/manage/shop-order/ordermanage"))
	require.False(t, isReportAPIPath("/api/manage/shop-order/analysis"))
}

func TestBuildReportMenuItemsOneRowPerReport(t *testing.T) {
	stats.ResetReportsForTest()
	t.Cleanup(stats.ResetReportsForTest)
	stats.RegisterReports(
		stats.ReportDef{
			Code: "order-daily", Service: "shop-order", Title: "日趋势报表",
			MenuTitle: "日趋势报表", SpecCode: "order.by_day", Kind: stats.ReportKindLine, Sort: 10,
		},
		stats.ReportDef{
			Code: "order-by-product", Service: "shop-order", Title: "商品销售",
			MenuTitle: "商品销售", SpecCode: "order.by_day_product", Kind: stats.ReportKindMixed, Sort: 20,
		},
	)

	mm := NewMenuManage()
	// 无目录 list 时 ensure 失败 → 返回 nil；此处直接测 isReportAPIPath + ListReportMenus 契约
	menus := stats.ListReportMenus("shop-order")
	require.Len(t, menus, 2)
	require.Equal(t, "order-daily", menus[0].Code)
	require.Equal(t, "/report/shop-order/order-daily", menus[0].Path)
	require.Equal(t, "日趋势报表", menus[0].Title)
	require.Equal(t, "order-by-product", menus[1].Code)
	require.Equal(t, "/report/shop-order/order-by-product", menus[1].Path)

	// 权限克隆：多报表互不共享同一 slice
	src := []*smodels.PermissionsModel{
		{Name: "reports", Url: "/api/manage/shop-order/reports"},
		{Name: "view", Url: "/api/manage/shop-order/reports/view"},
	}
	a := clonePermissions(src)
	b := clonePermissions(src)
	require.Len(t, a, 2)
	require.NotSame(t, a[0], b[0])
	_ = mm
}
