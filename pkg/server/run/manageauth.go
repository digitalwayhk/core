package run

import (
	"errors"
	"fmt"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// manageAuthAuthority 保存当前 HTMLServer 选定的 Manage Auth 权威服务。
// 本任务只负责选择与兼容校验；后续任务再据此注册同源认证路径。
type manageAuthAuthority struct {
	name    string
	context *router.ServiceContext
	router  *router.ServiceRouter
}

func resolveManageAuthAuthority(
	contexts []*router.ServiceContext,
	configured string,
) (*manageAuthAuthority, error) {
	eligible := manageAuthContexts(contexts)
	if len(eligible) == 0 {
		return nil, nil
	}
	if len(eligible) > 1 && strings.TrimSpace(configured) == "" {
		return nil, errors.New("多个 Manage 服务必须配置 ManageAuthAuthorityService")
	}
	authority, err := selectManageAuthContext(eligible, configured)
	if err != nil {
		return nil, err
	}
	if err := validateManageAuthCompatibility(authority, eligible); err != nil {
		return nil, err
	}
	return &manageAuthAuthority{
		name:    authority.Service.Name,
		context: authority,
		router:  authority.Router,
	}, nil
}

func manageAuthContexts(contexts []*router.ServiceContext) []*router.ServiceContext {
	eligible := make([]*router.ServiceContext, 0)
	for _, ctx := range contexts {
		if ctx == nil || ctx.Router == nil || ctx.Service == nil {
			continue
		}
		if len(ctx.Router.GetTypeRouters(types.ManageType)) == 0 {
			continue
		}
		eligible = append(eligible, ctx)
	}
	return eligible
}

func selectManageAuthContext(
	eligible []*router.ServiceContext,
	configured string,
) (*router.ServiceContext, error) {
	normalized := normalizeServiceName(configured)
	if normalized == "" {
		if len(eligible) == 1 {
			return eligible[0], nil
		}
		return nil, errors.New("多个 Manage 服务必须配置 ManageAuthAuthorityService")
	}
	for _, ctx := range eligible {
		if normalizeServiceName(ctx.Service.Name) == normalized {
			return ctx, nil
		}
	}
	return nil, fmt.Errorf("ManageAuthAuthorityService 指定的服务不存在：%s", strings.TrimSpace(configured))
}

func normalizeServiceName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func validateManageAuthCompatibility(
	authority *router.ServiceContext,
	eligible []*router.ServiceContext,
) error {
	if authority == nil || authority.Config == nil {
		return errors.New("Manage Auth 权威服务配置无效")
	}
	for _, ctx := range eligible {
		if ctx == authority {
			continue
		}
		if ctx == nil || ctx.Config == nil || ctx.Service == nil {
			return fmt.Errorf("服务配置无效，无法与权威服务 %s 比较", authority.Service.Name)
		}
		if err := compareManageAuthSecrets(authority, ctx); err != nil {
			return err
		}
	}
	if !authority.Config.ManageAuth.CasDoor.Enable || len(eligible) < 2 {
		return nil
	}
	for _, ctx := range eligible {
		if err := validateSharedCasdoorContract(authority, ctx); err != nil {
			return err
		}
	}
	return nil
}

func compareManageAuthSecrets(authority, other *router.ServiceContext) error {
	authManage := authority.Config.ManageAuth
	otherManage := other.Config.ManageAuth
	authName := authority.Service.Name
	otherName := other.Service.Name

	if authManage.AccessSecret != otherManage.AccessSecret {
		return incompatibleManageAuthField(otherName, authName, "ManageAuth.AccessSecret")
	}
	if authManage.AccessExpire != otherManage.AccessExpire {
		return incompatibleManageAuthField(otherName, authName, "ManageAuth.AccessExpire")
	}
	if authManage.RefreshSecret != otherManage.RefreshSecret {
		return incompatibleManageAuthField(otherName, authName, "ManageAuth.RefreshSecret")
	}
	if authManage.RefreshExpire != otherManage.RefreshExpire {
		return incompatibleManageAuthField(otherName, authName, "ManageAuth.RefreshExpire")
	}
	if authManage.CasDoor.Enable != otherManage.CasDoor.Enable {
		return incompatibleManageAuthField(otherName, authName, "ManageAuth.CasDoor.Enable")
	}
	return nil
}

func validateSharedCasdoorContract(authority, other *router.ServiceContext) error {
	authName := authority.Service.Name
	otherName := other.Service.Name

	if other.Config.AuthRevocation.Mode != config.AuthRevocationModeShared {
		return incompatibleManageAuthField(otherName, authName, "AuthRevocation.Mode")
	}
	if authority.Config.AuthRevocation.Mode != config.AuthRevocationModeShared {
		return incompatibleManageAuthField(authName, authName, "AuthRevocation.Mode")
	}

	authRedis := authority.Config.AuthRevocation.Redis
	otherRedis := other.Config.AuthRevocation.Redis
	if authRedis.Addr != otherRedis.Addr {
		return incompatibleManageAuthField(otherName, authName, "AuthRevocation.Redis.Addr")
	}
	if authRedis.Password != otherRedis.Password {
		return incompatibleManageAuthField(otherName, authName, "AuthRevocation.Redis.Password")
	}
	if authRedis.Prefix != otherRedis.Prefix {
		return incompatibleManageAuthField(otherName, authName, "AuthRevocation.Redis.Prefix")
	}

	if authority.Config.ManageAuth.CasDoor.WebhookSecret != other.Config.ManageAuth.CasDoor.WebhookSecret {
		return incompatibleManageAuthField(otherName, authName, "ManageAuth.CasDoor.WebhookSecret")
	}

	authData, err := authority.Config.ManageAuth.CasDoor.GetConfigData()
	if err != nil || authData == nil {
		return fmt.Errorf("权威服务 %s 加载 ManageAuth.CasDoor 配置失败", authName)
	}
	otherData, err := other.Config.ManageAuth.CasDoor.GetConfigData()
	if err != nil || otherData == nil {
		return fmt.Errorf("服务 %s 加载 ManageAuth.CasDoor 配置失败", otherName)
	}

	if authData.Server.Endpoint != otherData.Server.Endpoint {
		return incompatibleManageAuthField(otherName, authName, "ManageAuth.CasDoor.Endpoint")
	}
	if authData.Server.ClientID != otherData.Server.ClientID {
		return incompatibleManageAuthField(otherName, authName, "ManageAuth.CasDoor.ClientID")
	}
	if authData.Server.ClientSecret != otherData.Server.ClientSecret {
		return incompatibleManageAuthField(otherName, authName, "ManageAuth.CasDoor.ClientSecret")
	}
	if authData.Certificate != otherData.Certificate {
		return incompatibleManageAuthField(otherName, authName, "ManageAuth.CasDoor.Certificate")
	}
	if authData.Server.Organization != otherData.Server.Organization {
		return incompatibleManageAuthField(otherName, authName, "ManageAuth.CasDoor.Organization")
	}
	if authData.Server.Application != otherData.Server.Application {
		return incompatibleManageAuthField(otherName, authName, "ManageAuth.CasDoor.Application")
	}
	if authData.Server.FrontendURL != otherData.Server.FrontendURL {
		return incompatibleManageAuthField(otherName, authName, "ManageAuth.CasDoor.FrontendURL")
	}
	return nil
}

func incompatibleManageAuthField(serviceName, authorityName, field string) error {
	return fmt.Errorf("服务 %s 与权威服务 %s 的 %s 不兼容", serviceName, authorityName, field)
}
