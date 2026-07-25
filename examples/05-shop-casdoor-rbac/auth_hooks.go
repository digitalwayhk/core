package casdoorrbacshop

import (
	"context"
	"errors"
	"strings"

	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/business"
	"github.com/digitalwayhk/core/examples/05-shop-casdoor-rbac/contract"
	"github.com/digitalwayhk/core/pkg/server/types"
)

const (
	userRole       = "user"
	adminRole      = "administrator"
	orderScope     = "order"
	manageScope    = "manage"
	roleClaim      = "role"
	shopScopeClaim = "shop_scope"
)

// OnAuth 在框架签发 Token 前，根据已确认的认证域注入业务角色。
// 角色不从请求或 Casdoor 自定义字段中读取，避免调用方伪造。
func (*ShopService) OnAuth(_ context.Context, args *types.AuthHookArgs) error {
	if args == nil || args.Claims == nil || strings.TrimSpace(args.UID) == "" {
		return shopPermissionDenied(errors.New("认证签发参数不完整"))
	}
	role := ""
	scope := ""
	switch args.AuthType {
	case types.AuthTypeUser:
		role, scope = userRole, orderScope
	case types.AuthTypeManage:
		role, scope = adminRole, manageScope
	default:
		return shopPermissionDenied(errors.New("认证域不支持商城访问"))
	}
	args.Claims.AddData(roleClaim, role)
	args.Claims.AddData(shopScopeClaim, scope)
	args.Claims.AddData("example_service", contract.ServiceName)
	return nil
}

// OnAuthRequest 在验签和撤销校验后执行商城业务授权。
// Private 只接受普通用户域，Manage 只接受管理员域。
func (*ShopService) OnAuthRequest(ctx context.Context, args types.AuthRequestArgs) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(args.Identity.UID) == "" {
		return shopPermissionDenied(errors.New("已验证用户为空"))
	}
	role, roleOK := args.Claims[roleClaim].(string)
	scope, scopeOK := args.Claims[shopScopeClaim].(string)
	if !roleOK || !scopeOK {
		return shopPermissionDenied(errors.New("商城权限 Claim 缺失"))
	}
	switch args.PathType {
	case types.PrivateType:
		if args.Identity.AuthType == types.AuthTypeUser && role == userRole && scope == orderScope {
			return nil
		}
	case types.ManageType:
		if args.Identity.AuthType == types.AuthTypeManage && role == adminRole && scope == manageScope {
			return nil
		}
	}
	return shopPermissionDenied(errors.New("认证域与路由权限不匹配"))
}

// OnCasdoorEvent 在框架已持久化撤销事实后，异步写入商城身份审计。
// 该 Hook 不重复实现撤销权威，也不保存原始 Webhook。
func (own *ShopService) OnCasdoorEvent(ctx context.Context, event types.CasdoorEvent) error {
	own.identityEventsOnce.Do(func() {
		own.identityEvents = business.NewIdentityEventService()
	})
	return own.identityEvents.Record(ctx, event)
}

func shopPermissionDenied(cause error) error {
	return types.NewPublicError(types.ErrorKindForbidden, types.PublicCodeForbidden, "权限不足", cause)
}
