package supplierservice

import (
	"context"
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	privateapi "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/api/private"
	publicapi "github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/api/public"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/supplier-service/business"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// Service 组装供应商、商品及其跨服务查询路由。
type Service struct{}

func (*Service) ServiceName() string { return contract.SupplierServiceName }
func (*Service) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{
		&publicapi.GetProducts{}, &privateapi.GetProductSnapshot{}, &privateapi.AddProduct{}, &privateapi.SetProduct{}, &privateapi.GetMyProducts{},
	}
}
func (*Service) SubscribeRouters() []*servertypes.ObserveArgs { return nil }

// OnAuth 在 TestToken 签发前幂等建立供应商资料。
// 平台管理员是服务端固定身份，不会被误建为供应商。
func (*Service) OnAuth(_ context.Context, args *servertypes.AuthHookArgs) error {
	if args == nil || strings.TrimSpace(args.UID) == "" {
		return contract.ErrInvalidIdentity
	}
	if args.UID == contract.PlatformAdminUserID {
		return nil
	}
	_, err := business.EnsureSupplier(args.UID, args.Username)
	return err
}
