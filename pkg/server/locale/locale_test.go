// 本文件验证展示语言的规范化规则、请求头读取边界和标题回退顺序。
package locale

import (
	"net/http"
	"testing"

	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

// httpRequest 模拟实现 types.IRequestHttp 的管理端请求。
type httpRequest struct {
	types.IRequest
	raw *http.Request
}

func (own *httpRequest) GetHttpRequest() *http.Request {
	return own.raw
}

// plainRequest 模拟只实现 types.IRequest 的消费方 mock，不提供原始 http 请求。
type plainRequest struct {
	types.IRequest
}

func newHTTPRequest(t *testing.T, header, value string) *httpRequest {
	t.Helper()
	raw, err := http.NewRequest(http.MethodGet, "/api/servermanage/getmenu", nil)
	require.NoError(t, err)
	if header != "" {
		raw.Header.Set(header, value)
	}
	return &httpRequest{raw: raw}
}

func TestNormalizeMapsChineseTagsToZhCN(t *testing.T) {
	for _, raw := range []string{"zh", "zh-CN", "zh_CN", "zh-Hans", "ZH-cn", "  zh-cn  ", "zh-TW"} {
		require.Equal(t, ZhCN, Normalize(raw), "raw=%q", raw)
	}
}

func TestNormalizeMapsEnglishTagsToEnUS(t *testing.T) {
	for _, raw := range []string{"en", "en-US", "en_US", "EN-us", " en-us "} {
		require.Equal(t, EnUS, Normalize(raw), "raw=%q", raw)
	}
}

func TestNormalizeFallsBackToDefaultForUnsupportedTags(t *testing.T) {
	for _, raw := range []string{"", "   ", "ja-JP", "pt-BR", "not-a-locale", "-", "zzz"} {
		require.Equal(t, Default, Normalize(raw), "raw=%q", raw)
	}
	require.Equal(t, ZhCN, Default)
}

func TestFromRequestReadsHeaderCaseInsensitively(t *testing.T) {
	require.Equal(t, EnUS, FromRequest(newHTTPRequest(t, "x-locale", "en-US")))
	require.Equal(t, EnUS, FromRequest(newHTTPRequest(t, "X-LOCALE", "en_us")))
	require.Equal(t, ZhCN, FromRequest(newHTTPRequest(t, HeaderName, "zh-Hans")))
}

func TestFromRequestFallsBackWhenLocaleIsMissingOrUnreadable(t *testing.T) {
	require.Equal(t, ZhCN, FromRequest(newHTTPRequest(t, "", "")))
	require.Equal(t, ZhCN, FromRequest(newHTTPRequest(t, HeaderName, "")))
	require.Equal(t, ZhCN, FromRequest(&httpRequest{raw: nil}))
	require.Equal(t, ZhCN, FromRequest(nil))
}

// 消费方 mock 通常只实现 IRequest；解析语言不得因此 panic。
func TestFromRequestOnNonHTTPRequestReturnsDefault(t *testing.T) {
	require.NotPanics(t, func() {
		require.Equal(t, ZhCN, FromRequest(&plainRequest{}))
	})
}

// 查询参数和浏览器 Accept-Language 都不是语言权威源。
func TestFromRequestIgnoresQueryParameterAndAcceptLanguage(t *testing.T) {
	raw, err := http.NewRequest(http.MethodGet, "/api/servermanage/getmenu?locale=en-US", nil)
	require.NoError(t, err)
	raw.Header.Set("Accept-Language", "en-US,en;q=0.9")
	require.Equal(t, ZhCN, FromRequest(&httpRequest{raw: raw}))
}

func TestPickFallsBackToTheOtherLanguage(t *testing.T) {
	require.Equal(t, "Menus", Pick(EnUS, "菜单管理", "Menus"))
	require.Equal(t, "菜单管理", Pick(EnUS, "菜单管理", ""))
	require.Equal(t, "菜单管理", Pick(ZhCN, "菜单管理", "Menus"))
	require.Equal(t, "Menus", Pick(ZhCN, "", "Menus"))
	require.Equal(t, "", Pick(EnUS, "", ""))
}

// localeTitled 同时实现两种标题接口，验证 ILocaleTitle 优先。
type localeTitled struct {
	zh string
	en string
}

func (own *localeTitled) GetTitle() string { return "旧标题" }
func (own *localeTitled) GetLocaleTitle(current string) string {
	if current == EnUS {
		return own.en
	}
	return own.zh
}

// legacyTitled 只实现 ITitle，代表未声明本能力的旧服务。
type legacyTitled struct{}

func (own *legacyTitled) GetTitle() string { return "订单服务" }

func TestTitlePrefersLocaleTitle(t *testing.T) {
	instance := &localeTitled{zh: "代币管理", en: "Tokens"}
	require.Equal(t, "代币管理", Title(instance, ZhCN))
	require.Equal(t, "Tokens", Title(instance, EnUS))
}

// ILocaleTitle 该语言无文案时不得串用另一种语言的文案。
func TestTitleFallsBackToGetTitleOnlyForChinese(t *testing.T) {
	instance := &localeTitled{zh: "", en: ""}
	require.Equal(t, "旧标题", Title(instance, ZhCN))
	require.Equal(t, "", Title(instance, EnUS))
}

func TestTitleOnLegacyAndUntitledInstances(t *testing.T) {
	require.Equal(t, "订单服务", Title(&legacyTitled{}, ZhCN))
	require.Equal(t, "", Title(&legacyTitled{}, EnUS))
	require.Equal(t, "", Title(struct{}{}, ZhCN))
	require.Equal(t, "", Title(nil, ZhCN))
}
