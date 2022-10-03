package private

import (
	"errors"

	"github.com/digitalwayhk/core/examples/demo/models"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"

	"github.com/shopspring/decimal"
)

// AddOrder 新增订单
type AddOrder struct {
	Price   string `json:"price"`   //价格
	Amount  string `json:"amount"`  //数量
	TokenID uint   `json:"tokenid"` //币种ID
}

// 解析Order参数
func (own *AddOrder) Parse(req types.IRequest) error {
	//解析参数到结构体 {"amount": "123.432","price": "6454.45","tokenid": 338614647383365}
	return req.Bind(own)
}

// 验证新增Ordre是否允许调用,该方法返回nil，Do方法将被调用
func (own *AddOrder) Validation(req types.IRequest) error {
	if own.Price == "" {
		return errors.New("price is empty")
	}
	price, err := decimal.NewFromString(own.Price)
	if err != nil {
		return errors.New("price is not decimal")
	}
	if price.LessThan(decimal.NewFromFloat(0)) {
		return errors.New("price is less than zero")
	}
	if own.Amount == "" {
		return errors.New("amount is empty")
	}
	amount, err := decimal.NewFromString(own.Amount)
	if err != nil {
		return errors.New("amount is not decimal")
	}
	if amount.LessThan(decimal.NewFromFloat(0)) {
		return errors.New("amount is less than zero")
	}
	if u, _ := req.GetUser(); u == 0 {
		return errors.New("userid is empty")
	}
	if own.TokenID == 0 {
		return errors.New("tokenid is empty")
	}
	return nil
}

// 执行新增order逻辑,保存数据
func (own *AddOrder) Do(req types.IRequest) (interface{}, error) {
	//创建list
	list := entity.NewModelList[models.OrderModel](nil)
	//新建order
	order := list.NewItem()
	//获取token中的userid
	order.UserID, _ = req.GetUser()
	//设置价格
	order.Price, _ = decimal.NewFromString(own.Price)
	//设置数量
	order.Amount, _ = decimal.NewFromString(own.Amount)
	//设置币种ID
	order.TokenID = own.TokenID
	//添加到list
	err := list.Add(order)
	if err != nil {
		return nil, err
	}
	//通过存储方案持久化数据
	err = list.Save()
	return order, err
}

// RouterInfo路由注册信息
func (own *AddOrder) RouterInfo() *types.RouterInfo {
	//设置默认路由信息
	return router.DefaultRouterInfo(own)
}
