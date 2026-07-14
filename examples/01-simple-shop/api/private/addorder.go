package private

import (
	"strings"

	"github.com/digitalwayhk/core/examples/01-simple-shop/models"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// AddOrder 接收商品 ID 和数量，并以商品当前信息创建价格快照订单。
type AddOrder struct {
	ProductID uint `json:"productID"`
	Quantity  int  `json:"quantity"`
}

// Parse 绑定下单参数，UserID 不属于客户端可提交字段。
func (own *AddOrder) Parse(req servertypes.IRequest) error {
	return req.Bind(own)
}

// Validation 校验登录身份、商品 ID 和正数数量。
func (own *AddOrder) Validation(req servertypes.IRequest) error {
	userID, _ := req.GetUser()
	if strings.TrimSpace(userID) == "" {
		return models.NewBusinessError("用户身份无效")
	}
	if own.ProductID == 0 {
		return models.NewBusinessError("商品不存在")
	}
	if own.Quantity <= 0 {
		return models.NewValidationError("订单数量必须大于 0")
	}
	return nil
}

// Do 查询商品事实数据、保存订单，并在提交成功后发布新增通知。
func (own *AddOrder) Do(req servertypes.IRequest) (interface{}, error) {
	products := entity.NewModelList[models.Product](nil)
	product, err := products.SearchId(own.ProductID)
	if err != nil {
		return nil, err
	}
	if product == nil {
		return nil, models.NewBusinessError("商品不存在")
	}
	orders := entity.NewModelList[models.Order](nil)
	order := orders.NewItem()
	order.SetID(req.NewID())
	order.ProductID = product.ID
	order.ProductName = product.Name
	order.UnitPrice = product.Price
	order.Quantity = own.Quantity
	order.UserID, _ = req.GetUser()
	if err := orders.Add(order); err != nil {
		return nil, err
	}
	if err := orders.Save(); err != nil {
		return nil, err
	}
	notifyOrderChange(req, &OrderEvent{Action: "created", Order: order})
	return order, nil
}

// RouterInfo 将下单注册为需要认证的 POST 路由。
func (own *AddOrder) RouterInfo() *servertypes.RouterInfo {
	return router.DefaultRouterInfo(own)
}
