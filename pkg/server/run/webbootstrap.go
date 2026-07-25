package run

import (
	"encoding/json"
	"net/http"
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

// WebBootstrap 是前端启动所需的公开运行时能力描述，不包含密钥或 Token。
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
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(buildWebBootstrap(authority, r))
	})
}

func normalizeBootstrapAuthorityService(authority *manageAuthAuthority) string {
	if authority == nil {
		return ""
	}
	name := authority.name
	if strings.TrimSpace(name) == "" && authority.context != nil && authority.context.Service != nil {
		name = authority.context.Service.Name
	}
	return normalizeServiceName(name)
}

func buildWebBootstrap(authority *manageAuthAuthority, request *http.Request) WebBootstrap {
	mode := selectManageAuthMode(authority, request)
	response := WebBootstrap{
		SchemaVersion: webBootstrapSchemaVersion,
		Auth: WebBootstrapAuth{
			Mode:             mode,
			Type:             webAuthTypeManage,
			AuthorityService: normalizeBootstrapAuthorityService(authority),
		},
		Endpoints: WebBootstrapEndpoints{
			Callback: webBootstrapCallback,
			Refresh:  webBootstrapRefresh,
			OpenAPI:  webBootstrapOpenAPI,
		},
		UI: webBootstrapUIForMode(mode),
	}
	switch mode {
	case webAuthModeTestToken:
		response.Endpoints.AcquireToken = stringPointer(webBootstrapAcquireToken)
	case webAuthModeCasdoor:
		response.Endpoints.CasdoorConfig = stringPointer(webBootstrapCasdoorConfig)
	}
	return response
}

func selectManageAuthMode(authority *manageAuthAuthority, request *http.Request) string {
	if authority == nil || authority.context == nil || authority.context.Config == nil {
		return webAuthModeUnavailable
	}
	if authority.context.Config.ManageAuth.CasDoor.Enable {
		return webAuthModeCasdoor
	}
	if authority.router == nil || request == nil {
		return webAuthModeUnavailable
	}
	req := router.NewRequest(authority.router, request)
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
		return WebBootstrapUI{ShowLogin: true, ShowLogout: true}
	case webAuthModeTestToken:
		return WebBootstrapUI{ShowTestIdentity: true}
	default:
		return WebBootstrapUI{}
	}
}

func stringPointer(value string) *string {
	return &value
}
