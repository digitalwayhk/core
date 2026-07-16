package userservice

import (
	"context"
	"strings"

	"github.com/digitalwayhk/core/examples/06-shop-microservices/contract"
	privateapi "github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/api/private"
	publicapi "github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/api/public"
	"github.com/digitalwayhk/core/examples/06-shop-microservices/user-service/models"
	servertypes "github.com/digitalwayhk/core/pkg/server/types"
)

// Service 是买家唯一外部入口，不保存订单或商品权威副本。
type Service struct{}

func (*Service) ServiceName() string { return contract.UserServiceName }
func (*Service) Routers() []servertypes.IRouter {
	return []servertypes.IRouter{&publicapi.GetProducts{}, &privateapi.AddAddress{}, &privateapi.GetAddresses{}, &privateapi.DeleteAddress{}, &privateapi.AddOrder{}, &privateapi.GetOrders{}, &privateapi.DeleteOrder{}}
}
func (*Service) SubscribeRouters() []*servertypes.ObserveArgs { return nil }
func (*Service) OnAuth(_ context.Context, args *servertypes.AuthHookArgs) error {
	if args == nil || strings.TrimSpace(args.UID) == "" {
		return contract.ErrInvalidIdentity
	}
	_, err := models.EnsureUser(args.UID, args.Username)
	return err
}
