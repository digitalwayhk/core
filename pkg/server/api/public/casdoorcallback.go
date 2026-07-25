package public

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/casdoor/casdoor-go-sdk/casdoorsdk"
	"github.com/digitalwayhk/core/pkg/server/authstate"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/safe"
	casdoorauth "github.com/digitalwayhk/core/pkg/server/safe/casdoor"
	"github.com/digitalwayhk/core/pkg/server/types"
	"golang.org/x/oauth2"
)

const callbackPath = "/api/casdoor/callback"

// CasdoorCallback 使用指定认证域的 Casdoor Client 完成 OAuth 回调和框架 Token 签发。
type CasdoorCallback struct {
	Code  string `json:"code" form:"code" binding:"required"`
	State string `json:"state" form:"state" binding:"required"`
	Type  string `json:"type" desc:"Casdoor认证域，auth或manage"`
}

func (own *CasdoorCallback) Parse(req types.IRequest) error {
	own.Code = req.GetValue("code")
	own.State = req.GetValue("state")
	own.Type = req.GetValue("type")
	return nil
}

func (own *CasdoorCallback) Validation(req types.IRequest) error {
	authType, err := normalizeCasdoorAuthType(own.Type)
	if err != nil {
		return err
	}
	own.Type = string(authType)
	sc := router.GetContext(req.ServiceName())
	if _, err := casdoorClientForType(sc, authType); err != nil {
		return err
	}
	if strings.TrimSpace(own.Code) == "" {
		return errors.New("code is required")
	}
	if strings.TrimSpace(own.State) == "" {
		return errors.New("state is required")
	}
	return nil
}

func (own *CasdoorCallback) Do(req types.IRequest) (interface{}, error) {
	sc := router.GetContext(req.ServiceName())
	authType, err := normalizeCasdoorAuthType(own.Type)
	if err != nil {
		return nil, err
	}
	client, err := casdoorClientForType(sc, authType)
	if err != nil {
		return nil, authBoundaryError(err)
	}
	if sc.AuthRevocationManager == nil {
		return nil, authBoundaryError(authstate.ErrAuthorityUnavailable)
	}
	return authenticateCasdoorCallback(
		requestContext(req), sc, client, sc.AuthRevocationManager,
		authType, own.Code, own.State, time.Now().UTC(),
	)
}

func (*CasdoorCallback) GetResponse() interface{} { return &safe.TokenPairResponse{} }

func (own *CasdoorCallback) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfoWithOptions(own,
		router.WithMethod(http.MethodGet),
		router.WithPath(callbackPath),
		withAuthEndpointRateLimit(),
	)
}

type casdoorCallbackClient interface {
	Organization() string
	GetOAuthToken(string, string, ...casdoorsdk.OAuthOption) (*oauth2.Token, error)
	ParseJwtToken(string) (*casdoorsdk.Claims, error)
	GetUser(string) (*casdoorsdk.User, error)
}

type authIdentityAuthority interface {
	Current(context.Context, types.AuthIdentity) (authstate.State, error)
	ConfirmActive(context.Context, types.AuthIdentity, uint64) (authstate.State, error)
}

func authenticateCasdoorCallback(
	ctx context.Context,
	sc *router.ServiceContext,
	client casdoorCallbackClient,
	authority authIdentityAuthority,
	authType types.AuthType,
	code string,
	state string,
	now time.Time,
) (safe.TokenPairResponse, error) {
	if client == nil || authority == nil {
		return safe.TokenPairResponse{}, authBoundaryError(authstate.ErrAuthorityUnavailable)
	}
	token, err := client.GetOAuthToken(code, state)
	if err != nil || token == nil || strings.TrimSpace(token.AccessToken) == "" {
		return safe.TokenPairResponse{}, authBoundaryError(err)
	}
	claims, err := client.ParseJwtToken(token.AccessToken)
	if err != nil || claims == nil || strings.TrimSpace(claims.Name) == "" {
		return safe.TokenPairResponse{}, authBoundaryError(err)
	}
	user, err := client.GetUser(claims.Name)
	if err != nil || casdoorauth.VerifyActiveUser(user, client.Organization(), claims.Name) != nil || strings.TrimSpace(user.Id) == "" {
		return safe.TokenPairResponse{}, authBoundaryError(err)
	}
	authorityService := ""
	if sc != nil && sc.Service != nil {
		authorityService = strings.ToLower(strings.TrimSpace(sc.Service.Name))
	}
	identity := types.AuthIdentity{
		UID:              strings.TrimSpace(user.Id),
		Username:         casdoorUsername(user),
		AuthType:         authType,
		Provider:         types.AuthProviderCasdoor,
		ProviderSubject:  strings.TrimSpace(user.Name),
		AuthorityService: authorityService,
	}
	current, err := authority.Current(ctx, identity)
	if err != nil {
		return safe.TokenPairResponse{}, authBoundaryError(err)
	}
	// GetUser 已在线确认身份有效。即使旧会话曾被 logout 事件标记为 blocked，
	// 新登录也必须以当前世代原子解除阻断；旧 Token 仍因世代落后而继续失效。
	identity.Generation = current.Generation
	confirmed, err := authority.ConfirmActive(ctx, identity, current.Generation)
	if err != nil || confirmed.Blocked || confirmed.Generation != current.Generation {
		return safe.TokenPairResponse{}, authBoundaryError(err)
	}
	identity.Generation = confirmed.Generation
	return issueForServiceIdentityAt(ctx, sc, identity, types.AuthSourceCallback, claims, now)
}

func normalizeCasdoorAuthType(value string) (types.AuthType, error) {
	if strings.TrimSpace(value) == "" {
		return types.AuthTypeUser, nil
	}
	authType := types.AuthType(value)
	if authType != types.AuthTypeUser && authType != types.AuthTypeManage {
		return "", errors.New("casdoor auth type is invalid")
	}
	return authType, nil
}

func casdoorClientForType(sc *router.ServiceContext, authType types.AuthType) (*casdoorauth.DomainClient, error) {
	if sc == nil || sc.CasdoorClients == nil {
		return nil, errors.New("casdoor client is unavailable")
	}
	var client *casdoorauth.DomainClient
	switch authType {
	case types.AuthTypeUser:
		client = sc.CasdoorClients.Auth()
	case types.AuthTypeManage:
		client = sc.CasdoorClients.Manage()
	default:
		return nil, errors.New("casdoor auth type is invalid")
	}
	if client == nil {
		return nil, errors.New("casdoor authentication domain is disabled")
	}
	return client, nil
}

func casdoorUsername(user *casdoorsdk.User) string {
	if user == nil {
		return ""
	}
	for _, candidate := range []string{user.DisplayName, user.Email, user.Name} {
		if value := strings.TrimSpace(candidate); value != "" {
			return value
		}
	}
	return ""
}

func authBoundaryError(cause error) error {
	if cause == nil {
		cause = errors.New("authentication boundary rejected request")
	}
	return types.NewPublicError(types.ErrorKindUnauthenticated, types.PublicCodeUnauthenticated, "authentication failed", cause)
}

func casdoorCallbackPath() string { return callbackPath }
