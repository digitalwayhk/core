package public

import (
	"github.com/digitalwayhk/core/examples/demo/models"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type GetOrder struct {
}

//解析Order参数
func (this *GetOrder) Parse(req types.IRequest) error {
	return nil
}

//验证新增Ordre是否允许调用,该方法返回nil，Do方法将被调用
func (this *GetOrder) Validation(req types.IRequest) error {
	return nil
}

//执行新增order逻辑,保存数据
func (this *GetOrder) Do(req types.IRequest) (interface{}, error) {
	//创建model容器
	list := entity.NewModelList[models.OrderModel](nil)
	//获取默认查询参数
	item := list.GetSearchItem()
	//查询数据
	err := list.LoadList(item)
	return list.ToArray(), err
}

//RouterInfo路由注册信息
func (this *GetOrder) RouterInfo() *types.RouterInfo {
	//设置默认路由信息
	return router.DefaultRouterInfo(this)
}
