package manage_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	st "github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// localeRequest 带 X-Locale 头，用于验证 View.Do 的语言挑选。
type localeRequest struct {
	crudRequest
	header string
}

func (r *localeRequest) GetHttpRequest() *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/manage/demo/ordermanage/view", nil)
	if r.header != "" {
		req.Header.Set("X-Locale", r.header)
	}
	return req
}

// localeManageSvc 是实现 ILocaleTitle 的 Manage 控制器。
type localeManageSvc struct {
	*testManageSvc[testItem]
	zh string
	en string
}

func newLocaleManageSvc(zh, en string) *localeManageSvc {
	return &localeManageSvc{
		testManageSvc: newTestManageSvc[testItem](nil),
		zh:            zh,
		en:            en,
	}
}

func (s *localeManageSvc) GetLocaleTitle(locale string) string {
	if locale == "en-US" {
		return s.en
	}
	return s.zh
}

func (s *localeManageSvc) Routers() []st.IRouter {
	return []st.IRouter{&fakeCommandRouter{structName: "Add[testItem]"}}
}

// fakeCommandRouter 直接给出 RouterInfo：manage 的路由注册要求包路径含 api 段，
// 外部测试包无法通过 RouterInfo() 构造真实路由。
type fakeCommandRouter struct {
	structName string
}

func (r *fakeCommandRouter) Parse(st.IRequest) error             { return nil }
func (r *fakeCommandRouter) Validation(st.IRequest) error        { return nil }
func (r *fakeCommandRouter) Do(st.IRequest) (interface{}, error) { return nil, nil }
func (r *fakeCommandRouter) RouterInfo() *st.RouterInfo {
	return &st.RouterInfo{StructName: r.structName, ServiceName: "demo"}
}

func commandRouterInfo(structName string) *st.RouterInfo {
	return (&fakeCommandRouter{structName: structName}).RouterInfo()
}

func fieldTitle(t *testing.T, vm *view.ViewModel, propField string) string {
	t.Helper()
	for _, field := range vm.Fields {
		if field.PropField == propField {
			return field.Title
		}
	}
	t.Fatalf("field %s not found in view model", propField)
	return ""
}

func commandTitle(t *testing.T, vm *view.ViewModel, command string) string {
	t.Helper()
	for _, cmd := range vm.Commands {
		if cmd.Command == command {
			return cmd.Title
		}
	}
	t.Fatalf("command %s not found in view model", command)
	return ""
}

func doView(t *testing.T, svc *localeManageSvc, req st.IRequest) *view.ViewModel {
	t.Helper()
	result, err := manage.NewView[testItem](svc).Do(req)
	require.NoError(t, err)
	vm, ok := result.(*view.ViewModel)
	require.True(t, ok, "View.Do should return *view.ViewModel")
	return vm
}

// TestRouterToCommandKeepsStableKeysAndUsesDefaultLanguage 固定公开 API 的默认语言，
// Command 与 Name 是稳定键，只有 Title 随语言变。
func TestRouterToCommandKeepsStableKeysAndUsesDefaultLanguage(t *testing.T) {
	cmd := manage.RouterToCommand(commandRouterInfo("Add[testItem]"))

	require.NotNil(t, cmd)
	assert.Equal(t, "add", cmd.Command)
	assert.Equal(t, "Add", cmd.Name)
	assert.Equal(t, "新增", cmd.Title)
}

func TestRouterToLocaleCommandPicksTitleByLanguage(t *testing.T) {
	info := commandRouterInfo("Add[testItem]")

	cases := []struct {
		locale string
		title  string
	}{
		{locale: "zh-CN", title: "新增"},
		{locale: "en-US", title: "Add"},
		{locale: "ja-JP", title: "新增"},
		{locale: "", title: "新增"},
	}
	for _, tc := range cases {
		cmd := manage.RouterToLocaleCommand(info, tc.locale)

		require.NotNil(t, cmd, "locale %q", tc.locale)
		assert.Equal(t, tc.title, cmd.Title, "locale %q", tc.locale)
		assert.Equal(t, "add", cmd.Command, "locale %q", tc.locale)
	}
}

func TestRouterToLocaleCommandSkipsViewAndSearch(t *testing.T) {
	assert.Nil(t, manage.RouterToLocaleCommand(commandRouterInfo("View[testItem]"), "en-US"))
	assert.Nil(t, manage.RouterToLocaleCommand(commandRouterInfo("Search[testItem]"), "en-US"))
}

// TestRouterToLocaleCommandKeepsCustomCommandTypeName 自定义命令不在标准表里，保持类型名。
func TestRouterToLocaleCommandKeepsCustomCommandTypeName(t *testing.T) {
	cmd := manage.RouterToLocaleCommand(commandRouterInfo("Approve[testItem]"), "en-US")

	require.NotNil(t, cmd)
	assert.Equal(t, "Approve", cmd.Title)
	assert.Equal(t, "approve", cmd.Command)
}

// TestViewDoLocalizesTitleCommandsAndCommonFields 覆盖 §9.5：title、标准命令和公共字段
// 都要随 X-Locale 变化，同时 Name 与 Field 这些稳定键保持不变。
func TestViewDoLocalizesTitleCommandsAndCommonFields(t *testing.T) {
	svc := newLocaleManageSvc("订单管理", "Order Management")

	zh := doView(t, svc, &localeRequest{header: "zh-CN"})
	en := doView(t, svc, &localeRequest{header: "en-US"})

	assert.Equal(t, "订单管理", zh.Title)
	assert.Equal(t, "Order Management", en.Title)

	assert.Equal(t, "新增", commandTitle(t, zh, "add"))
	assert.Equal(t, "Add", commandTitle(t, en, "add"))

	assert.Equal(t, "编号", fieldTitle(t, zh, "ID"))
	assert.Equal(t, "ID", fieldTitle(t, en, "ID"))
	assert.Equal(t, "创建时间", fieldTitle(t, zh, "CreatedAt"))
	assert.Equal(t, "Created At", fieldTitle(t, en, "CreatedAt"))
	assert.Equal(t, "更新时间", fieldTitle(t, zh, "UpdatedAt"))
	assert.Equal(t, "Updated At", fieldTitle(t, en, "UpdatedAt"))

	// 稳定键不随语言变
	assert.Equal(t, zh.Name, en.Name)
	assert.Equal(t, "Name", fieldTitle(t, zh, "Name"), "非公共字段保持 Go 字段名")
	assert.Equal(t, "Name", fieldTitle(t, en, "Name"), "非公共字段保持 Go 字段名")
}

// TestViewDoFallsBackToChineseWithoutLocaleHeader 锁定 §8：没有 X-Locale 时行为与本能力之前一致。
func TestViewDoFallsBackToChineseWithoutLocaleHeader(t *testing.T) {
	svc := newLocaleManageSvc("订单管理", "Order Management")

	vm := doView(t, svc, &localeRequest{})

	assert.Equal(t, "订单管理", vm.Title)
	assert.Equal(t, "新增", commandTitle(t, vm, "add"))
	assert.Equal(t, "创建时间", fieldTitle(t, vm, "CreatedAt"))
}

// TestViewDoAcceptsNonHttpRequest 覆盖 §9.1：非 IRequestHttp 的 mock 请求回退中文且不 panic。
func TestViewDoAcceptsNonHttpRequest(t *testing.T) {
	svc := newLocaleManageSvc("订单管理", "Order Management")

	vm := doView(t, svc, &crudRequest{})

	assert.Equal(t, "订单管理", vm.Title)
	assert.Equal(t, "编号", fieldTitle(t, vm, "ID"))
}

// TestViewDoKeepsTypeNameWithoutLocaleTitle 未实现 ILocaleTitle 的服务保持原有标题行为。
func TestViewDoKeepsTypeNameWithoutLocaleTitle(t *testing.T) {
	svc := newTestManageSvc[testItem](nil)

	result, err := manage.NewView[testItem](svc).Do(&localeRequest{header: "en-US"})

	require.NoError(t, err)
	vm := result.(*view.ViewModel)
	assert.Equal(t, vm.Name, vm.Title)
}

// TestViewDoLocaleTitleFallsBackWhenTranslationMissing 某语言无文案时保持原标题。
func TestViewDoLocaleTitleFallsBackWhenTranslationMissing(t *testing.T) {
	svc := newLocaleManageSvc("订单管理", "")

	vm := doView(t, svc, &localeRequest{header: "en-US"})

	assert.Equal(t, vm.Name, vm.Title, "英文文案为空时不得把标题写成空串")
}
