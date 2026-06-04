package manage

import (
	"fmt"

	"github.com/digitalwayhk/core/examples/demo/api/manage/button"
	"github.com/digitalwayhk/core/examples/demo/models"
	st "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// OrderManage 演示以下管理服务高级模式：
//  1. 多层继承：嵌入 AppManage[T]（应用层）而非直接嵌入 manage.ManageService[T]
//  2. 选择性路由：Routers() 在只读基础上追加写操作和自定义按钮
//  3. SearchBefore 内存数据源：返回 true 跳过数据库查询
//  4. ViewFieldModel 默认全隐藏：先全关 Visible，再逐字段显式开启
//  5. ViewCommandModel：配置按钮属性（IsSplit 配对 / IsAlert / IsSelectRow）
//  6. IDoBefore 钩子：AddBefore / RemoveBefore 做业务验证，无需 type switch
type OrderManage struct {
	*AppManage[models.OrderModel]
}

func NewOrderManage() *OrderManage {
	own := &OrderManage{}
	own.AppManage = NewAppManage[models.OrderModel](own)
	return own
}

// Routers 在 AppManage 的只读路由（View + Search）基础上追加写操作和自定义按钮。
func (own *OrderManage) Routers() []st.IRouter {
	routers := own.AppManage.Routers()                                      // [View, Search]
	routers = append(routers, own.Add, own.Edit, own.Remove)                // 写操作
	routers = append(routers, own.Submit, own.Release)                      // 状态操作
	routers = append(routers, button.NewExportData[models.OrderModel](own)) // 自定义按钮
	return routers
}

// ViewModel 设置标题，须先调用上层。
func (own *OrderManage) ViewModel(v *view.ViewModel) {
	own.AppManage.ViewModel(v) // ← 先调用上层（设置 AutoLoad=true）
	v.Title = "订单管理"
}

// SearchBefore 演示内存数据源短路模式：
// 存在内存缓存时直接返回，第三个返回值 true 告知框架跳过数据库查询。
func (own *OrderManage) SearchBefore(sender interface{}, req st.IRequest) (interface{}, error, bool) {
	if _, ok := sender.(*manage.Search[models.OrderModel]); ok {
		if cached := getOrderCache(); cached != nil {
			return &view.TableData{
				Rows:  cached,
				Total: int64(len(cached)),
			}, nil, true // true = 跳过 DB，直接返回
		}
	}
	return nil, nil, false // false = 继续走数据库查询
}

// ViewFieldModel 演示默认全隐藏 + 按需显示模式。
// 须先调用上层（隐藏系统字段），再统一关闭 Visible，最后按需开启。
func (own *OrderManage) ViewFieldModel(model interface{}, field *view.FieldModel) {
	own.AppManage.ViewFieldModel(model, field) // ← 先调用上层
	field.Visible = false                      // 默认全部隐藏，避免新字段被意外暴露
	field.IsSearch = false
	field.IsEdit = false
	switch {
	case field.IsFieldOrTitle("Price"):
		field.Title = "价格"
		field.Visible = true
		field.IsSearch = true
		field.IsEdit = true
		field.Sorter = true
		field.Index = 1
	case field.IsFieldOrTitle("Amount"):
		field.Title = "数量"
		field.Visible = true
		field.IsEdit = true
		field.Index = 2
	case field.IsFieldOrTitle("UserID"):
		field.Title = "用户"
		field.Visible = true
		field.IsSearch = true
		field.Index = 3
	case field.IsFieldOrTitle("State"):
		field.Title = "状态"
		field.Visible = true
		field.IsEdit = false
		field.ComBox("待提交", "已提交", "已发布") // 枚举值
		field.Index = 4
	}
}

// ViewCommandModel 演示按钮属性配置，包括 IsSplit 配对。
// IsSplit=true + SplitName 将两个互斥操作合并到同一个按钮位：
// 前端根据当前行状态只显示其中一个（submit 对应 State=0，release 对应 State=1）。
func (own *OrderManage) ViewCommandModel(cmd *view.CommandModel) {
	switch cmd.Command {
	case "submit":
		cmd.IsSplit = true // 与 release 共用一个按钮位
		cmd.SplitName = "release"
		cmd.IsSelectRow = true
		cmd.IsAlert = true
	case "exportdata":
		cmd.IsAlert = false     // 导出无需二次确认
		cmd.IsSelectRow = false // 导出不需要选中行
		cmd.Title = "导出订单"
	}
}

// AddBefore 实现 IDoBefore.AddBefore，新增前做业务验证。
// AppManage.DoBefore 会自动分派到此方法，无需手写 type switch。
func (own *OrderManage) AddBefore(add *manage.Add[models.OrderModel], req st.IRequest) (interface{}, error, bool) {
	if add.Model != nil && add.Model.Price.IsZero() {
		return nil, fmt.Errorf("价格不能为零"), true // true = 阻止继续执行
	}
	return nil, nil, false
}

// RemoveBefore 实现 IDoBefore.RemoveBefore，已提交的订单不允许删除。
func (own *OrderManage) RemoveBefore(remove *manage.Remove[models.OrderModel], req st.IRequest) (interface{}, error, bool) {
	if remove.Model != nil && remove.Model.State > 0 {
		return nil, fmt.Errorf("已提交的订单不能删除"), true
	}
	return nil, nil, false
}

// orderCache 演示内存缓存（实际项目可能是 Redis / sync.Map 等）。
var orderCache []*models.OrderModel

func getOrderCache() []*models.OrderModel {
	return orderCache
}
