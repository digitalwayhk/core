// 本文件提供管理端展示语言的请求头解析与规范化能力。
// 语言只用于展示文案挑选，不参与路由、权限和数据边界判断。
package locale

import (
	"strings"

	"github.com/digitalwayhk/core/pkg/server/types"
)

const (
	// HeaderName 是管理端携带当前展示语言的请求头名称。
	HeaderName = "X-Locale"
	// ZhCN 是简体中文展示语言。
	ZhCN = "zh-CN"
	// EnUS 是英文展示语言。
	EnUS = "en-US"
	// Default 是无法识别请求语言时的缺省展示语言。
	Default = ZhCN
)

// exactLocales 覆盖管理端会真实发送的取值，避免完全依赖子标签推断。
var exactLocales = map[string]string{
	"zh":      ZhCN,
	"zh-cn":   ZhCN,
	"zh-hans": ZhCN,
	"en":      EnUS,
	"en-us":   EnUS,
}

// primaryLocales 按主语言子标签兜底，未列出的语言一律回退 Default。
var primaryLocales = map[string]string{
	"zh": ZhCN,
	"en": EnUS,
}

// Normalize 把任意语言标记规范化为受支持的展示语言。
// 大小写不敏感，`_` 等价于 `-`；无法识别时返回 Default，不返回错误。
func Normalize(raw string) string {
	tag := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(raw, "_", "-")))
	if tag == "" {
		return Default
	}
	if matched, ok := exactLocales[tag]; ok {
		return matched
	}
	primary := tag
	if index := strings.Index(tag, "-"); index > 0 {
		primary = tag[:index]
	}
	if matched, ok := primaryLocales[primary]; ok {
		return matched
	}
	return Default
}

// FromRequest 读取当前请求的展示语言。
// 请求未实现 types.IRequestHttp 或无原始请求时返回 Default，不 panic。
func FromRequest(req types.IRequest) string {
	if req == nil {
		return Default
	}
	httpReq, ok := req.(types.IRequestHttp)
	if !ok {
		return Default
	}
	raw := httpReq.GetHttpRequest()
	if raw == nil || raw.Header == nil {
		return Default
	}
	return Normalize(raw.Header.Get(HeaderName))
}

// Pick 按当前语言在中英文案之间挑选，缺失文案回退到另一种语言。
// 两者都为空时返回空串，由调用方决定更下层的回退。
func Pick(current, zh, en string) string {
	if current == EnUS {
		if strings.TrimSpace(en) != "" {
			return en
		}
		return zh
	}
	if strings.TrimSpace(zh) != "" {
		return zh
	}
	return en
}

// Title 按当前语言从实现方读取展示标题。
// 优先 ILocaleTitle.GetLocaleTitle(current)，为空时回退 ITitle.GetTitle()（默认中文），
// 仍为空时返回空串，由调用方回退到 Name 或类型名。
func Title(instance interface{}, current string) string {
	if instance == nil {
		return ""
	}
	if localeTitle, ok := instance.(types.ILocaleTitle); ok {
		if title := strings.TrimSpace(localeTitle.GetLocaleTitle(current)); title != "" {
			return title
		}
	}
	if current != ZhCN {
		return ""
	}
	if title, ok := instance.(types.ITitle); ok {
		return strings.TrimSpace(title.GetTitle())
	}
	return ""
}
