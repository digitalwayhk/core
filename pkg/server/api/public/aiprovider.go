// 本文件提供 ServerManage 域下的 AI 提供商配置与同源 LLM 代理，供管理端 PageAgent 使用。
package public

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/aiprovider"
	"github.com/digitalwayhk/core/pkg/server/api"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// AIProvider 读取 AI 提供商配置。
// view=admin（默认）脱敏密钥并展示上游 baseURL；view=runtime 返回同源代理 baseURL，不下发密钥。
type AIProvider struct {
	api.ServerArgs
	View string `json:"view"`
}

// Parse 绑定 view 查询/体参数。
func (own *AIProvider) Parse(req types.IRequest) error {
	if err := own.ServerArgs.Parse(req); err != nil {
		return err
	}
	if err := req.Bind(own); err != nil {
		return err
	}
	if v := req.GetValue("view"); v != "" {
		own.View = v
	}
	if own.View == "" {
		own.View = "admin"
	}
	return nil
}

// Validation 校验 view 取值。
func (own *AIProvider) Validation(req types.IRequest) error {
	if err := own.ServerArgs.Validation(req); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(own.View)) {
	case "admin", "runtime":
		return nil
	default:
		return errors.New("view must be admin or runtime")
	}
}

// Do 返回配置视图。
func (own *AIProvider) Do(req types.IRequest) (interface{}, error) {
	cfg, err := aiprovider.Load()
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(own.View, "runtime") {
		return aiprovider.RuntimeView(cfg), nil
	}
	return aiprovider.AdminView(cfg), nil
}

// RouterInfo 注册为 ServerManage 路由。
func (own *AIProvider) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfoWithOptions(own,
		router.WithPathType(types.ServerManagerType),
		withSystemEndpointRateLimit(),
	)
}

// SaveAIProvider 保存 AI 提供商配置。
type SaveAIProvider struct {
	api.ServerArgs
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"baseURL"`
	APIKey   string `json:"apiKey"`
	Language string `json:"language"`
}

// Parse 绑定配置体。
func (own *SaveAIProvider) Parse(req types.IRequest) error {
	if err := own.ServerArgs.Parse(req); err != nil {
		return err
	}
	return req.Bind(own)
}

// Validation 基础校验；启用时的字段完整性由 aiprovider.Save 完成。
func (own *SaveAIProvider) Validation(req types.IRequest) error {
	return own.ServerArgs.Validation(req)
}

// Do 持久化配置并返回管理页脱敏视图。
func (own *SaveAIProvider) Do(req types.IRequest) (interface{}, error) {
	saved, err := aiprovider.Save(aiprovider.Config{
		Enabled:  own.Enabled,
		Provider: own.Provider,
		Model:    own.Model,
		BaseURL:  own.BaseURL,
		APIKey:   own.APIKey,
		Language: own.Language,
	})
	if err != nil {
		return nil, err
	}
	return aiprovider.AdminView(saved), nil
}

// RouterInfo 注册为 ServerManage 路由。
func (own *SaveAIProvider) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfoWithOptions(own,
		router.WithPathType(types.ServerManagerType),
		withSystemEndpointRateLimit(),
	)
}

// TestAIProvider 探测 LLM 连通性（OpenAI 兼容 chat/completions）。
// 可传表单草稿；apiKey 为空或 ******** 时使用已保存密钥。
type TestAIProvider struct {
	api.ServerArgs
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"baseURL"`
	APIKey   string `json:"apiKey"`
	Language string `json:"language"`
}

// Parse 绑定探测参数。
func (own *TestAIProvider) Parse(req types.IRequest) error {
	if err := own.ServerArgs.Parse(req); err != nil {
		return err
	}
	return req.Bind(own)
}

// Validation 校验本地访问/鉴权。
func (own *TestAIProvider) Validation(req types.IRequest) error {
	return own.ServerArgs.Validation(req)
}

// Do 执行探测并返回结果（不含密钥）。
func (own *TestAIProvider) Do(req types.IRequest) (interface{}, error) {
	cfg, err := aiprovider.MergeProbeInput(aiprovider.Config{
		Provider: own.Provider,
		Model:    own.Model,
		BaseURL:  own.BaseURL,
		APIKey:   own.APIKey,
		Language: own.Language,
	})
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	if req != nil {
		// 若请求带有可取消上下文则使用（兼容无 Context 的测试桩）
		type withCtx interface{ Context() context.Context }
		if c, ok := req.(withCtx); ok && c.Context() != nil {
			ctx = c.Context()
		}
	}
	return aiprovider.Probe(ctx, cfg), nil
}

// RouterInfo 注册为 ServerManage 路由。
func (own *TestAIProvider) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfoWithOptions(own,
		router.WithPathType(types.ServerManagerType),
		withSystemEndpointRateLimit(),
	)
}

// AILLMChatCompletions 是 PageAgent 的同源 OpenAI 兼容代理。
// 路径固定为 /api/servermanage/aillm/chat/completions；响应为上游原始 JSON（非框架 envelope）。
type AILLMChatCompletions struct {
	api.ServerArgs
	rawBody []byte
}

// Parse 读取原始 JSON 请求体（不 Bind 到字段，避免破坏 page-agent 工具调用结构）。
func (own *AILLMChatCompletions) Parse(req types.IRequest) error {
	if err := own.ServerArgs.Parse(req); err != nil {
		return err
	}
	httpReq, ok := req.(types.IRequestHttp)
	if !ok || httpReq.GetHttpRequest() == nil {
		return errors.New("AILLM 代理仅支持 HTTP 请求")
	}
	r := httpReq.GetHttpRequest()
	if r.Body == nil || r.Body == http.NoBody {
		return errors.New("请求体不能为空")
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		return err
	}
	own.rawBody = body
	return nil
}

// Validation 校验鉴权/本机访问策略。
func (own *AILLMChatCompletions) Validation(req types.IRequest) error {
	return own.ServerArgs.Validation(req)
}

// Do 转发到已配置上游 LLM。
func (own *AILLMChatCompletions) Do(req types.IRequest) (interface{}, error) {
	ctx := context.Background()
	if req != nil {
		type withCtx interface{ Context() context.Context }
		if c, ok := req.(withCtx); ok && c.Context() != nil {
			ctx = c.Context()
		} else if httpReq, ok := req.(types.IRequestHttp); ok && httpReq.GetHttpRequest() != nil {
			ctx = httpReq.GetHttpRequest().Context()
		}
	}
	return aiprovider.ProxyChatCompletions(ctx, own.rawBody)
}

// Clean 归还对象池前清空请求体缓冲。
func (own *AILLMChatCompletions) Clean() {
	own.rawBody = nil
	own.ServiceName = ""
}

// Reset 下一次使用前恢复零值。
func (own *AILLMChatCompletions) Reset() {
	own.rawBody = nil
	own.ServiceName = ""
}

// RouterInfo 注册固定代理路径，使用原始 OpenAI 响应写出。
func (own *AILLMChatCompletions) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfoWithOptions(own,
		router.WithPath(aiprovider.ChatCompletionsPath),
		router.WithPathType(types.ServerManagerType),
		router.WithMethod(http.MethodPost),
		// page-agent 多轮调用较密，放宽限流。
		router.WithExternalRateLimit(30, 60),
		router.WithResponseHandler(aillmChatCompletionsResponse),
	)
}

// aillmChatCompletionsResponse 成功时透传上游状态码与 body；失败时返回 OpenAI 风格 error JSON。
func aillmChatCompletionsResponse(w http.ResponseWriter, _ *http.Request, res types.IResponse) {
	w.Header().Set("Cache-Control", "private, no-store")
	if res == nil {
		writeOpenAIProxyError(w, http.StatusInternalServerError, "empty response")
		return
	}
	if !res.GetSuccess() {
		contract := types.ResolvePublicError(res.GetError())
		if setter, ok := res.(types.ISetPublicError); ok {
			setter.SetPublicError(contract.Code, contract.Message)
		}
		status := contract.HTTPStatus
		if status < http.StatusBadRequest {
			status = http.StatusBadRequest
		}
		writeOpenAIProxyError(w, status, contract.Message)
		return
	}
	result := extractChatProxyResult(res.GetData())
	if result == nil {
		writeOpenAIProxyError(w, http.StatusInternalServerError, "invalid proxy result")
		return
	}
	ct := strings.TrimSpace(result.ContentType)
	if ct == "" {
		ct = "application/json"
	}
	w.Header().Set("Content-Type", ct)
	status := result.StatusCode
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	if len(result.Body) > 0 {
		_, _ = w.Write(result.Body)
	}
}

func extractChatProxyResult(data interface{}) *aiprovider.ChatProxyResult {
	switch v := data.(type) {
	case *aiprovider.ChatProxyResult:
		return v
	case aiprovider.ChatProxyResult:
		return &v
	default:
		return nil
	}
}

func writeOpenAIProxyError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	if status < http.StatusBadRequest {
		status = http.StatusInternalServerError
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]interface{}{
			"message": message,
			"type":    "aillm_proxy_error",
		},
	})
}
