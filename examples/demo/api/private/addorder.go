package private

import (
	"errors"

	"github.com/digitalwayhk/core/examples/demo/models"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"

	"github.com/shopspring/decimal"
)

//AddOrder 新增订单
type AddOrder struct {
	Price  string `json:"price"`  //价格
	Amount string `json:"amount"` //数量
}

//解析Order参数
func (this *AddOrder) Parse(req types.IRequest) error {
	return req.Bind(this)
}

//验证新增Ordre是否允许调用,该方法返回nil，Do方法将被调用
func (this *AddOrder) Validation(req types.IRequest) error {
	if this.Price == "" {
		return errors.New("price is empty")
	}
	price, err := decimal.NewFromString(this.Price)
	if err != nil {
		return errors.New("price is not decimal")
	}
	if price.LessThan(decimal.NewFromFloat(0)) {
		return errors.New("price is less than zero")
	}
	if this.Amount == "" {
		return errors.New("amount is empty")
	}
	amount, err := decimal.NewFromString(this.Amount)
	if err != nil {
		return errors.New("amount is not decimal")
	}
	if amount.LessThan(decimal.NewFromFloat(0)) {
		return errors.New("amount is less than zero")
	}
	if u, _ := req.GetUser(); u == 0 {
		return errors.New("userid is empty")
	}
	return nil
}

//执行新增order逻辑,保存数据
func (this *AddOrder) Do(req types.IRequest) (interface{}, error) {
	//创建model容器
	list := entity.NewModelList[models.OrderModel](nil)
	//新建order
	order := list.NewItem()
	//获取token中的userid
	order.UserID, _ = req.GetUser()
	price, _ := decimal.NewFromString(this.Price)
	//设置价格
	order.Price = price
	amount, _ := decimal.NewFromString(this.Amount)
	//设置数量
	order.Amount = amount
	//添加到容器
	err := list.Add(order)
	if err != nil {
		return nil, err
	}
	//持久化到数据库
	err = list.Save()
	return order, err
}

//RouterInfo路由注册信息
func (this *AddOrder) RouterInfo() *types.RouterInfo {
	//设置默认路由信息
	return router.DefaultRouterInfo(this)
}
