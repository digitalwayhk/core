package run

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/api/public"
	"github.com/digitalwayhk/core/pkg/server/router"
)

const (
	webBootstrapSchemaVersion = 1
	webBootstrapPath          = "/api/web/bootstrap"

	webAuthModeCasdoor     = "casdoor"
	webAuthModeTestToken   = "test_token"
	webAuthModeUnavailable = "unavailable"
	webAuthTypeManage      = "manage"

	webBootstrapAcquireToken  = "/api/servermanage/testtoken?userid=12345&type=1"
	webBootstrapCasdoorConfig = "/api/casdoor?type=manage"
	webBootstrapCallback      = "/callback"
	webBootstrapRefresh       = "/api/refresh"
	webBootstrapOpenAPI       = "/swagger/"
)

// WebBootstrap 是前端启动所需的公开运行时能力描述，不得包含密钥或 Token。
type WebBootstrap struct {
	SchemaVersion int                   `json:"schema_version"`
	Auth          WebBootstrapAuth      `json:"auth"`
	Endpoints     WebBootstrapEndpoints `json:"endpoints"`
	UI            WebBootstrapUI        `json:"ui"`
}

type WebBootstrapAuth struct {
	Mode             string `json:"mode"`
	Type             string `json:"type"`
	AuthorityService string `json:"authority_service"`
}

type WebBootstrapEndpoints struct {
	AcquireToken  *string `json:"acquire_token"`
	CasdoorConfig *string `json:"casdoor_config"`
	Callback      string  `json:"callback"`
	Refresh       string  `json:"refresh"`
	OpenAPI       string  `json:"openapi"`
}

type WebBootstrapUI struct {
	ShowLogin        bool `json:"show_login"`
	ShowLogout       bool `json:"show_logout"`
	ShowTestIdentity bool `json:"show_test_identity"`
}

func newWebBootstrapHandler(authority *manageAuthAuthority) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 所有响应均不可缓存（含 405）。
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		response := buildWebBootstrap(authority, r)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(response)
	})
}

// normalizeBootstrapAuthorityService 与 Casdoor Token Claim / Callback 一致：
// strings.ToLower(strings.TrimSpace(name))。
func normalizeBootstrapAuthorityService(authority *manageAuthAuthority) string {
	if authority == nil {
		return ""
	}
	name := authority.name
	if strings.TrimSpace(name) == "" && authority.context != nil && authority.context.Service != nil {
		name = authority.context.Service.Name
	}
	return strings.ToLower(strings.TrimSpace(name))
}

func buildWebBootstrap(authority *manageAuthAuthority, r *http.Request) WebBootstrap {
	mode := selectManageAuthMode(authority, r)
	authorityService := normalizeBootstrapAuthorityService(authority)
	response := WebBootstrap{
		SchemaVersion: webBootstrapSchemaVersion,
		Auth: WebBootstrapAuth{
			Mode:             mode,
			Type:             webAuthTypeManage,
			AuthorityService: authorityService,
		},
		Endpoints: WebBootstrapEndpoints{
			Callback: webBootstrapCallback,
			Refresh:  authEndpointForService(webBootstrapRefresh, authorityService),
			OpenAPI:  webBootstrapOpenAPI,
		},
		UI: webBootstrapUIForMode(mode),
	}
	switch mode {
	case webAuthModeTestToken:
		response.Endpoints.AcquireToken = stringPtr(authEndpointForService(webBootstrapAcquireToken, authorityService))
	case webAuthModeCasdoor:
		response.Endpoints.CasdoorConfig = stringPtr(authEndpointForService(webBootstrapCasdoorConfig, authorityService))
	}
	return response
}

func authEndpointForService(endpoint, service string) string {
	service = normalizeServiceName(service)
	if service == "" {
		return endpoint
	}
	separator := "?"
	if strings.Contains(endpoint, "?") {
		separator = "&"
	}
	return endpoint + separator + "service=" + url.QueryEscape(service)
}

func selectManageAuthMode(authority *manageAuthAuthority, r *http.Request) string {
	if authority == nil || authority.context == nil || authority.context.Config == nil {
		return webAuthModeUnavailable
	}
	if authority.context.Config.ManageAuth.CasDoor.Enable {
		return webAuthModeCasdoor
	}
	if authority.router == nil || r == nil {
		return webAuthModeUnavailable
	}
	// 仅评估 TestToken 本地访问策略，不实际签发 Token。
	req := router.NewRequest(authority.router, r)
	if req == nil {
		return webAuthModeUnavailable
	}
	if err := (&public.TestToken{}).Validation(req); err != nil {
		return webAuthModeUnavailable
	}
	return webAuthModeTestToken
}

func webBootstrapUIForMode(mode string) WebBootstrapUI {
	switch mode {
	case webAuthModeCasdoor:
		return WebBootstrapUI{ShowLogin: true, ShowLogout: true, ShowTestIdentity: false}
	case webAuthModeTestToken:
		return WebBootstrapUI{ShowLogin: false, ShowLogout: false, ShowTestIdentity: true}
	default:
		return WebBootstrapUI{ShowLogin: false, ShowLogout: false, ShowTestIdentity: false}
	}
}

func stringPtr(value string) *string {
	return &value
}
