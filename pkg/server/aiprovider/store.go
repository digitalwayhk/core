// Package aiprovider 提供管理端 PageAgent 使用的 AI 提供商运行时配置持久化。
// 配置按方案 A 下发给已认证的 ServerManage 前端；API Key 会进入浏览器，仅适合受控内网。
package aiprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/digitalwayhk/core/pkg/utils"
)

const (
	// FileName 是相对进程工作目录 etc 下的配置文件名。
	FileName = "aiprovider.json"
	// MaskedAPIKey 是管理页展示用的脱敏占位，保存时若收到该值则保留旧密钥。
	MaskedAPIKey = "********"
)

// Config 是 AI 提供商运行时配置。
type Config struct {
	Enabled  bool   `json:"enabled"`
	Provider string `json:"provider"` // dashscope | openai | custom
	Model    string `json:"model"`
	BaseURL  string `json:"baseURL"`
	APIKey   string `json:"apiKey"`
	Language string `json:"language"`
}

// View 是下发给前端的视图。
// Runtime 含完整密钥供 PageAgent 使用；Admin 脱敏密钥。
type View struct {
	Enabled    bool   `json:"enabled"`
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	BaseURL    string `json:"baseURL"`
	APIKey     string `json:"apiKey"`
	APIKeySet  bool   `json:"apiKeySet"`
	Language   string `json:"language"`
	ConfigPath string `json:"configPath,omitempty"`
}

var (
	mu       sync.RWMutex
	pathHook func() string // 测试可注入
)

// DefaultConfig 返回空配置默认值。
func DefaultConfig() Config {
	return Config{
		Enabled:  false,
		Provider: "dashscope",
		Model:    "qwen3.5-plus",
		BaseURL:  "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Language: "zh-CN",
	}
}

// ConfigPath 返回配置文件绝对路径。
func ConfigPath() string {
	if pathHook != nil {
		return pathHook()
	}
	return filepath.Join(utils.Getpath(), "etc", FileName)
}

// SetConfigPathForTest 仅测试使用。
func SetConfigPathForTest(path string) {
	pathHook = func() string { return path }
}

// ResetPathHookForTest 清除测试路径钩子。
func ResetPathHookForTest() {
	pathHook = nil
}

// Load 读取配置；文件不存在时返回默认配置。
func Load() (Config, error) {
	mu.RLock()
	defer mu.RUnlock()
	return loadUnlocked()
}

func loadUnlocked() (Config, error) {
	path := ConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return Config{}, err
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	normalize(&cfg)
	return cfg, nil
}

// Save 校验并写入配置文件（目录 0700，文件 0600）。
// 若 apiKey 为空或为脱敏占位，则保留磁盘上的旧密钥。
func Save(incoming Config) (Config, error) {
	mu.Lock()
	defer mu.Unlock()

	existing, err := loadUnlocked()
	if err != nil {
		return Config{}, err
	}

	normalize(&incoming)
	if incoming.APIKey == "" || incoming.APIKey == MaskedAPIKey {
		incoming.APIKey = existing.APIKey
	}
	if err := validate(incoming); err != nil {
		return Config{}, err
	}

	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Config{}, err
	}
	raw, err := json.MarshalIndent(incoming, "", "  ")
	if err != nil {
		return Config{}, err
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return Config{}, err
	}
	return incoming, nil
}

// AdminView 返回管理页脱敏视图。
func AdminView(cfg Config) View {
	normalize(&cfg)
	v := View{
		Enabled:    cfg.Enabled,
		Provider:   cfg.Provider,
		Model:      cfg.Model,
		BaseURL:    cfg.BaseURL,
		APIKeySet:  strings.TrimSpace(cfg.APIKey) != "",
		Language:   cfg.Language,
		ConfigPath: ConfigPath(),
	}
	if v.APIKeySet {
		v.APIKey = MaskedAPIKey
	}
	return v
}

// RuntimeView 返回 PageAgent 运行时完整配置（含密钥）。
func RuntimeView(cfg Config) View {
	normalize(&cfg)
	return View{
		Enabled:   cfg.Enabled,
		Provider:  cfg.Provider,
		Model:     cfg.Model,
		BaseURL:   cfg.BaseURL,
		APIKey:    cfg.APIKey,
		APIKeySet: strings.TrimSpace(cfg.APIKey) != "",
		Language:  cfg.Language,
	}
}

// ReadyForAgent 判断配置是否足以启动 PageAgent。
func ReadyForAgent(cfg Config) bool {
	normalize(&cfg)
	return cfg.Enabled &&
		strings.TrimSpace(cfg.Model) != "" &&
		strings.TrimSpace(cfg.BaseURL) != ""
}

func normalize(cfg *Config) {
	if cfg == nil {
		return
	}
	cfg.Provider = strings.TrimSpace(cfg.Provider)
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.Language = strings.TrimSpace(cfg.Language)
	if cfg.Provider == "" {
		cfg.Provider = "custom"
	}
	if cfg.Language == "" {
		cfg.Language = "zh-CN"
	}
}

func validate(cfg Config) error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.Model == "" {
		return errors.New("启用时 model 不能为空")
	}
	if cfg.BaseURL == "" {
		return errors.New("启用时 baseURL 不能为空")
	}
	return nil
}

// ProbeResult 是连通性探测结果（不含密钥）。
type ProbeResult struct {
	OK         bool   `json:"ok"`
	StatusCode int    `json:"statusCode,omitempty"`
	Message    string `json:"message"`
	LatencyMs  int64  `json:"latencyMs"`
	Endpoint   string `json:"endpoint,omitempty"`
}

// Probe 使用 OpenAI 兼容 chat/completions 发一条极短请求验证 baseURL/model/apiKey。
// 不会把 apiKey 写入返回值。
func Probe(ctx context.Context, cfg Config) ProbeResult {
	normalize(&cfg)
	if cfg.Model == "" || cfg.BaseURL == "" {
		return ProbeResult{OK: false, Message: "model 与 baseURL 不能为空"}
	}
	endpoint, err := chatCompletionsURL(cfg.BaseURL)
	if err != nil {
		return ProbeResult{OK: false, Message: err.Error()}
	}
	body := map[string]interface{}{
		"model": cfg.Model,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
		"max_tokens": 1,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return ProbeResult{OK: false, Message: err.Error()}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return ProbeResult{OK: false, Message: err.Error(), Endpoint: endpoint}
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return ProbeResult{
			OK:        false,
			Message:   "请求失败: " + err.Error(),
			LatencyMs: latency,
			Endpoint:  endpoint,
		}
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return ProbeResult{
			OK:         true,
			StatusCode: resp.StatusCode,
			Message:    "连通成功",
			LatencyMs:  latency,
			Endpoint:   endpoint,
		}
	}
	msg := strings.TrimSpace(string(respBody))
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	if msg == "" {
		msg = resp.Status
	}
	return ProbeResult{
		OK:         false,
		StatusCode: resp.StatusCode,
		Message:    fmt.Sprintf("HTTP %d: %s", resp.StatusCode, msg),
		LatencyMs:  latency,
		Endpoint:   endpoint,
	}
}

// MergeProbeInput 把探测请求与已存配置合并：密钥占位或空则用已存密钥。
func MergeProbeInput(incoming Config) (Config, error) {
	existing, err := Load()
	if err != nil {
		return Config{}, err
	}
	normalize(&incoming)
	if incoming.Model == "" {
		incoming.Model = existing.Model
	}
	if incoming.BaseURL == "" {
		incoming.BaseURL = existing.BaseURL
	}
	if incoming.APIKey == "" || incoming.APIKey == MaskedAPIKey {
		incoming.APIKey = existing.APIKey
	}
	if incoming.Provider == "" {
		incoming.Provider = existing.Provider
	}
	if incoming.Language == "" {
		incoming.Language = existing.Language
	}
	normalize(&incoming)
	if incoming.Model == "" || incoming.BaseURL == "" {
		return Config{}, errors.New("model 与 baseURL 不能为空（可先填写表单或保存配置）")
	}
	return incoming, nil
}

func chatCompletionsURL(base string) (string, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return "", errors.New("baseURL 为空")
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("baseURL 无效: %s", base)
	}
	if strings.HasSuffix(base, "/chat/completions") {
		return base, nil
	}
	return base + "/chat/completions", nil
}
