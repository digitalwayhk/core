package performanceshop

import (
	"github.com/digitalwayhk/core/examples/04-shop-performance/api/manage"
	privateapi "github.com/digitalwayhk/core/examples/04-shop-performance/api/private"
	publicapi "github.com/digitalwayhk/core/examples/04-shop-performance/api/public"
	"github.com/digitalwayhk/core/examples/04-shop-performance/contract"
	"github.com/digitalwayhk/core/examples/04-shop-performance/models"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/zeromicro/go-zero/core/logx"
)

// ShopService 组装模型与 Manage 继承能力完整示例。
type ShopService struct{}

// ServiceName 返回继承商城的稳定服务名。
func (own *ShopService) ServiceName() string { return contract.ServiceName }

// Routers 返回继承商城的 Manage、Public 和 Private 路由。
func (own *ShopService) Routers() []types.IRouter {
	routers := make([]types.IRouter, 0, 36)
	routers = append(routers, manage.NewProductManage().Routers()...)
	routers = append(routers, manage.NewSupplierManage().Routers()...)
	routers = append(routers, manage.NewPaymentTypeManage().Routers()...)
	routers = append(routers, manage.NewOrderManage().Routers()...)
	routers = append(routers, manage.NewPaymentRecordManage().Routers()...)
	routers = append(routers,
		&publicapi.GetProducts{},
		&publicapi.GetSuppliers{},
		&publicapi.GetPaymentTypes{},
		&privateapi.AddOrder{},
		&privateapi.GetOrders{},
		&privateapi.DeleteOrder{},
		&privateapi.CreatePayment{},
		&privateapi.CancelOrder{},
	)
	return routers
}

// SubscribeRouters 返回内部服务观察订阅；本示例没有跨服务订阅。
func (own *ShopService) SubscribeRouters() []*types.ObserveArgs { return nil }

// Start 启动订单本地持久与 SQLite 写后同步。
func (own *ShopService) Start() {
	if err := models.StartOrderWriteStore(); err != nil {
		logx.Errorw("shop_order_write_store_start_failed", logx.Field("error", err))
	}
}

// Stop 在服务关闭时汇合积压并释放 Badger。
func (own *ShopService) Stop() {
	if err := models.StopOrderWriteStore(); err != nil {
		logx.Errorw("shop_order_write_store_stop_failed", logx.Field("error", err))
	}
}
