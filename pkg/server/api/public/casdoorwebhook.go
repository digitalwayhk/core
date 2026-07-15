package public

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/server/authstate"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

const (
	casdoorWebhookPath    = "/api/casdoor/webhook"
	maxCasdoorWebhookBody = 64 << 10
)

// CasdoorWebhook 接收 Auth 或 Manage 域的 Casdoor 身份事件。
// 请求成功仅表示撤销权威已持久化且可靠控制事件已被 EventBridge 接受；
// 服务的 OnCasdoorEvent 由持久化 worker 异步重试，不阻塞 Casdoor。
type CasdoorWebhook struct {
	event     types.CasdoorEvent
	retention time.Duration
}

type CasdoorWebhookResponse struct {
	Accepted   bool   `json:"accepted"`
	Generation uint64 `json:"generation"`
}

func (own *CasdoorWebhook) Parse(req types.IRequest) error {
	httpRequest, ok := req.(types.IRequestHttp)
	if !ok || httpRequest.GetHttpRequest() == nil {
		return webhookUnavailableError(errors.New("Webhook缺少HTTP请求上下文"))
	}
	sc := router.GetContext(req.ServiceName())
	if sc == nil || sc.Config == nil {
		return webhookUnavailableError(errors.New("Webhook服务上下文不可用"))
	}
	value, retention, err := parseCasdoorWebhookRequest(httpRequest.GetHttpRequest(), req.ServiceName(), sc.Config)
	if err != nil {
		return err
	}
	own.event = value
	own.retention = retention
	return nil
}

func (*CasdoorWebhook) Validation(types.IRequest) error { return nil }

func (own *CasdoorWebhook) Do(req types.IRequest) (interface{}, error) {
	sc := router.GetContext(req.ServiceName())
	if sc == nil || sc.AuthRevocationManager == nil {
		return nil, webhookUnavailableError(authstate.ErrAuthorityUnavailable)
	}
	result, err := sc.AuthRevocationManager.ProcessEvent(requestContext(req), own.event, own.retention)
	if err != nil {
		if errors.Is(err, authstate.ErrInvalidEvent) {
			return nil, webhookValidationError(err)
		}
		return nil, webhookUnavailableError(err)
	}
	return &CasdoorWebhookResponse{Accepted: true, Generation: result.State.Generation}, nil
}

func (*CasdoorWebhook) GetResponse() interface{} { return &CasdoorWebhookResponse{} }

func (own *CasdoorWebhook) RouterInfo() *types.RouterInfo {
	return router.DefaultRouterInfoWithOptions(own,
		router.WithMethod(http.MethodPost),
		router.WithPath(casdoorWebhookPath),
		withAuthEndpointRateLimit(),
	)
}

type casdoorWebhookPayload struct {
	ID           int                        `json:"id"`
	Name         string                     `json:"name"`
	CreatedTime  string                     `json:"createdTime"`
	Organization string                     `json:"organization"`
	Application  string                     `json:"application"`
	User         string                     `json:"user"`
	Action       string                     `json:"action"`
	ExtendedUser casdoorWebhookExtendedUser `json:"extendedUser"`
}

type casdoorWebhookExtendedUser struct {
	ID                string `json:"id"`
	Owner             string `json:"owner"`
	Name              string `json:"name"`
	SignupApplication string `json:"signupApplication"`
	IsForbidden       bool   `json:"isForbidden"`
	IsDeleted         bool   `json:"isDeleted"`
}

func parseCasdoorWebhookRequest(request *http.Request, serviceName string, serverConfig *config.ServerConfig) (types.CasdoorEvent, time.Duration, error) {
	if request == nil || request.Body == nil || serverConfig == nil {
		return types.CasdoorEvent{}, 0, webhookValidationError(errors.New("Webhook请求无效"))
	}
	if request.ContentLength > maxCasdoorWebhookBody {
		return types.CasdoorEvent{}, 0, webhookValidationError(errors.New("Webhook请求体过大"))
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return types.CasdoorEvent{}, 0, webhookValidationError(errors.New("Webhook Content-Type无效"))
	}
	authType, err := normalizeRequiredCasdoorAuthType(request.URL.Query().Get("type"))
	if err != nil {
		return types.CasdoorEvent{}, 0, webhookValidationError(err)
	}
	domain, refreshExpire, err := casdoorWebhookDomain(serverConfig, authType)
	if err != nil {
		return types.CasdoorEvent{}, 0, webhookValidationError(err)
	}
	if !verifyWebhookSecret(request.Header.Get("Authorization"), domain.WebhookSecret) {
		return types.CasdoorEvent{}, 0, webhookAuthenticationError(errors.New("Webhook认证失败"))
	}

	body, err := io.ReadAll(io.LimitReader(request.Body, maxCasdoorWebhookBody+1))
	if err != nil || len(body) > maxCasdoorWebhookBody {
		return types.CasdoorEvent{}, 0, webhookValidationError(errors.New("Webhook请求体无效"))
	}
	payload := casdoorWebhookPayload{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return types.CasdoorEvent{}, 0, webhookValidationError(errors.New("Webhook JSON无效"))
	}
	data, err := domain.GetConfigData()
	if err != nil || data == nil {
		return types.CasdoorEvent{}, 0, webhookUnavailableError(errors.New("Casdoor配置不可用"))
	}
	value, err := normalizeCasdoorWebhookPayload(payload, strings.TrimSpace(serviceName), authType, data)
	if err != nil {
		return types.CasdoorEvent{}, 0, webhookValidationError(err)
	}
	retention := time.Duration(refreshExpire) * time.Second
	if retention <= 0 {
		return types.CasdoorEvent{}, 0, webhookUnavailableError(errors.New("Refresh Token有效期配置无效"))
	}
	return value, retention, nil
}

func normalizeRequiredCasdoorAuthType(value string) (types.AuthType, error) {
	switch strings.TrimSpace(value) {
	case string(types.AuthTypeUser):
		return types.AuthTypeUser, nil
	case string(types.AuthTypeManage):
		return types.AuthTypeManage, nil
	default:
		return "", errors.New("Webhook认证域无效")
	}
}

func casdoorWebhookDomain(serverConfig *config.ServerConfig, authType types.AuthType) (*config.CasDoorConfig, int64, error) {
	switch authType {
	case types.AuthTypeUser:
		if !serverConfig.Auth.CasDoor.Enable {
			return nil, 0, errors.New("Casdoor Auth域未启用")
		}
		return &serverConfig.Auth.CasDoor, serverConfig.Auth.RefreshExpire, nil
	case types.AuthTypeManage:
		if !serverConfig.ManageAuth.CasDoor.Enable {
			return nil, 0, errors.New("Casdoor Manage域未启用")
		}
		return &serverConfig.ManageAuth.CasDoor, serverConfig.ManageAuth.RefreshExpire, nil
	default:
		return nil, 0, errors.New("Webhook认证域无效")
	}
}

func normalizeCasdoorWebhookPayload(payload casdoorWebhookPayload, serviceName string, authType types.AuthType, data *config.CasDoorConfigData) (types.CasdoorEvent, error) {
	if serviceName == "" || data == nil {
		return types.CasdoorEvent{}, errors.New("Webhook服务配置无效")
	}
	organization := strings.TrimSpace(data.Server.Organization)
	application := strings.TrimSpace(data.Server.Application)
	if strings.TrimSpace(payload.Organization) != organization {
		return types.CasdoorEvent{}, errors.New("Webhook组织不匹配")
	}
	if owner := strings.TrimSpace(payload.ExtendedUser.Owner); owner != "" && owner != organization {
		return types.CasdoorEvent{}, errors.New("Webhook用户组织不匹配")
	}
	payloadApplication := strings.TrimSpace(payload.Application)
	if payloadApplication == "" {
		payloadApplication = strings.TrimSpace(payload.ExtendedUser.SignupApplication)
	}
	if payloadApplication != application {
		return types.CasdoorEvent{}, errors.New("Webhook应用不匹配")
	}
	subject := strings.TrimSpace(payload.ExtendedUser.Name)
	user := strings.TrimSpace(payload.User)
	if subject == "" {
		subject = user
	}
	if subject == "" || (user != "" && user != subject) {
		return types.CasdoorEvent{}, errors.New("Webhook用户标识无效")
	}
	uid := strings.TrimSpace(payload.ExtendedUser.ID)
	if uid == "" {
		return types.CasdoorEvent{}, errors.New("Webhook UID不能为空")
	}
	occurredAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(payload.CreatedTime))
	if err != nil {
		return types.CasdoorEvent{}, errors.New("Webhook事件时间无效")
	}
	eventType := strings.ToLower(strings.TrimSpace(payload.Action))
	value := types.CasdoorEvent{
		ServiceName: serviceName, AuthType: authType, Provider: types.AuthProviderCasdoor,
		ProviderSubject: subject, UID: uid, EventType: eventType,
		EventOrder: occurredAt.UnixNano(), Blocked: payload.ExtendedUser.IsForbidden || payload.ExtendedUser.IsDeleted,
		OccurredAt: occurredAt.UTC(),
	}
	if payload.ExtendedUser.IsDeleted {
		value.Blocked = true
	}
	if _, err := validateWebhookEventType(value); err != nil {
		return types.CasdoorEvent{}, err
	}
	value.ID = casdoorWebhookEventID(serviceName, authType, payload, value)
	return value, nil
}

func validateWebhookEventType(value types.CasdoorEvent) (types.CasdoorEvent, error) {
	switch value.EventType {
	case "login", "signup", "logout", "sso-logout", "update-user", "delete-user", "unlink":
		return value, nil
	default:
		return types.CasdoorEvent{}, errors.New("Webhook事件类型无效")
	}
}

func casdoorWebhookEventID(serviceName string, authType types.AuthType, payload casdoorWebhookPayload, value types.CasdoorEvent) string {
	sourceID := strings.TrimSpace(payload.Name)
	if sourceID == "" && payload.ID > 0 {
		sourceID = strconv.Itoa(payload.ID)
	}
	identity := sourceID
	if identity == "" {
		identity = fmt.Sprintf("%s|%s|%s|%d|%t", value.EventType, value.ProviderSubject, value.UID, value.EventOrder, value.Blocked)
	}
	sum := sha256.Sum256([]byte(serviceName + "|" + string(authType) + "|" + identity))
	return hex.EncodeToString(sum[:])
}

func verifyWebhookSecret(header, expected string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) || strings.TrimSpace(expected) == "" {
		return false
	}
	actual := []byte(strings.TrimSpace(strings.TrimPrefix(header, prefix)))
	wanted := []byte(expected)
	return len(actual) == len(wanted) && subtle.ConstantTimeCompare(actual, wanted) == 1
}

func webhookValidationError(cause error) error {
	return types.NewPublicError(types.ErrorKindValidation, types.PublicCodeValidation, "invalid request", cause)
}

func webhookAuthenticationError(cause error) error {
	return types.NewPublicError(types.ErrorKindUnauthenticated, types.PublicCodeUnauthenticated, "authentication failed", cause)
}

func webhookUnavailableError(cause error) error {
	return types.NewPublicError(types.ErrorKindUnavailable, types.PublicCodeUnavailable, "service unavailable", cause)
}
