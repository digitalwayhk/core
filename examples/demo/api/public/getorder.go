package public

import (
	"github.com/digitalwayhk/core/examples/demo/models"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// GetOrder 获取通过AddOrder API接口添加的订单
type GetOrder struct {
	Page int `json:"page"`
	Size int `json:"size"`
}

// Parse 解析通过http传递的参数
func (own *GetOrder) Parse(req types.IRequest) error {
	//req.GetValue("id") //获取参数 url?id=1
	//req.GetClaims("userid") //获取jwt中的claims

	//绑定json参数到结构体 {page:1,size:10}
	return req.Bind(own)
}

// Validation 验证方法,该方法返回nil，Do方法将被调用
func (own *GetOrder) Validation(req types.IRequest) error {
	if own.Page == 0 {
		own.Page = 1
	}
	if own.Size == 0 {
		own.Size = 10
	}
	return nil
}

// Do 执行逻辑
func (own *GetOrder) Do(req types.IRequest) (interface{}, error) {
	//创建list
	list := entity.NewModelList[models.OrderModel](nil)
	//查询order表中的数据，查询第Page页，每页Size条数据
	items, _, err := list.SearchAll(own.Page, own.Size)
	//查询数据
	return items, err
}

// RouterInfo路由注册信息
func (own *GetOrder) RouterInfo() *types.RouterInfo {
	//设置默认路由信息
	return router.DefaultRouterInfo(own)
}
