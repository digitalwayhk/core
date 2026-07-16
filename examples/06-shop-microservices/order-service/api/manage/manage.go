package manage

import (
	"errors"
	"strconv"

	orderdto "github.com/digitalwayhk/core/examples/06-shop-microservices/dto/order"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/business"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/order-service/models"
	"github.com/digitalwayhk/core/pkg/server/router"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

type PaymentTypeManage struct {
	*managepkg.ManageService[models.PaymentType]
}

func NewPaymentTypeManage() *PaymentTypeManage {
	own := &PaymentTypeManage{}
	own.ManageService = managepkg.NewManageService[models.PaymentType](own)
	return own
}
func (*PaymentTypeManage) ViewModel(model *view.ViewModel) {
	model.Title = "支付类型管理"
	model.AutoLoad = true
}

type OrderManage struct {
	*managepkg.ManageService[models.Order]
}

func NewOrderManage() *OrderManage {
	own := &OrderManage{}
	own.ManageService = managepkg.NewManageService[models.Order](own)
	return own
}
func (o *OrderManage) Routers() []servertypes.IRouter { return []servertypes.IRouter{o.View, o.Search} }
func (*OrderManage) ViewModel(model *view.ViewModel) {
	model.Title = "订单查询"
	model.AutoLoad = true
}

type PaymentRecordManage struct {
	*managepkg.ManageService[models.PaymentRecord]
}

func NewPaymentRecordManage() *PaymentRecordManage {
	own := &PaymentRecordManage{}
	own.ManageService = managepkg.NewManageService[models.PaymentRecord](own)
	return own
}
func (p *PaymentRecordManage) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{p.View, p.Search}
}
func (*PaymentRecordManage) ViewModel(model *view.ViewModel) {
	model.Title = "支付流水查询"
	model.AutoLoad = true
}

// ConfirmPayment 是管理员确认第三方已支付的受控状态命令。
type ConfirmPayment struct {
	PaymentID uint `json:"paymentID"`
}

func (c *ConfirmPayment) Parse(req servertypes.IRequest) error { return req.Bind(c) }
func (c *ConfirmPayment) Validation(servertypes.IRequest) error {
	if c.PaymentID == 0 {
		return errors.New("支付流水 ID 不能为空")
	}
	return nil
}
func (c *ConfirmPayment) Do(req servertypes.IRequest) (interface{}, error) {
	return business.ConfirmPayment(c.PaymentID, strconv.FormatUint(uint64(req.NewID()), 10))
}
func (*ConfirmPayment) GetResponse() interface{} { return &orderdto.Order{} }
func (c *ConfirmPayment) RouterInfo() *servertypes.RouterInfo {
	return router.DefaultRouterInfoWithOptions(c, router.WithPath("/api/manage/shop-order/confirmpayment"))
}
