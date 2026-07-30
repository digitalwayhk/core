// 本文件提供 ServerManage 域下的 AI 提供商配置读写，供管理端 PageAgent 运行时下发（方案 A）。
package public

import (
	"errors"
	"strings"

	"github.com/digitalwayhk/core/pkg/server/aiprovider"
	"github.com/digitalwayhk/core/pkg/server/api"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// AIProvider 读取 AI 提供商配置。
// view=admin（默认）脱敏密钥；view=runtime 返回完整密钥供 PageAgent 使用。
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
