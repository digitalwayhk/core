// 本文件提供 Core 控制面管理员主体的只读身份与启停管理页面。
package manage

import (
	"errors"

	"github.com/digitalwayhk/core/pkg/server/smodels"
	servertype "github.com/digitalwayhk/core/pkg/server/types"
	manageservice "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
)

// ManagePrincipalManage 展示由可信 Casdoor callback 建立的管理员主体。
type ManagePrincipalManage struct {
	*DmpBase[smodels.ManagePrincipalModel]
}

// NewManagePrincipalManage 创建 Core 管理员主体页面。
func NewManagePrincipalManage() *ManagePrincipalManage {
	own := &ManagePrincipalManage{}
	own.DmpBase = NewDmpBase[smodels.ManagePrincipalModel](own)
	return own
}

// Routers 只开放查看、搜索和启停编辑；主体不能由页面新增或删除。
func (own *ManagePrincipalManage) Routers() []servertype.IRouter {
	return []servertype.IRouter{own.View, own.Search, own.Edit}
}

// GetLocaleTitle 返回管理员主体页面标题。
func (*ManagePrincipalManage) GetLocaleTitle(locale string) string {
	if locale == "en-US" {
		return "Administrators"
	}
	return "管理员"
}

// DoBefore 在标准 Edit 写入前恢复服务端可信身份字段，只允许页面修改 Enabled。
// 首位管理员必须始终保持启用，以免控制面失去唯一的系统管理员入口。
func (*ManagePrincipalManage) DoBefore(
	sender interface{},
	_ servertype.IRequest,
) (interface{}, error, bool) {
	edit, ok := sender.(*manageservice.Edit[smodels.ManagePrincipalModel])
	if !ok || edit == nil || edit.Model == nil || edit.OldItem == nil {
		return nil, nil, false
	}
	requested, previous := edit.Model, edit.OldItem
	if previous.IsFirst && !requested.Enabled {
		return nil, servertype.NewPublicError(
			servertype.ErrorKindForbidden,
			servertype.PublicCodeForbidden,
			"permission denied",
			errors.New("bootstrap manage principal cannot be disabled"),
		), true
	}
	requested.Code = previous.Code
	requested.Username = previous.Username
	requested.Provider = previous.Provider
	requested.ProviderSubject = previous.ProviderSubject
	requested.IsFirst = previous.IsFirst
	requested.BootstrapSlot = previous.BootstrapSlot
	return nil, nil, false
}

// ViewFieldModel 将可信身份字段设为只读，只允许修改 Enabled。
func (own *ManagePrincipalManage) ViewFieldModel(model interface{}, field *view.FieldModel) {
	own.DmpBase.ViewFieldModel(model, field)
	switch {
	case field.IsFieldOrTitle("code"):
		field.Title = "管理员编码"
		field.Disabled = true
	case field.IsFieldOrTitle("username"):
		field.Title = "用户名"
		field.Disabled = true
	case field.IsFieldOrTitle("provider"):
		field.Title = "认证提供方"
		field.Disabled = true
	case field.IsFieldOrTitle("providersubject"):
		field.Title = "认证主体"
		field.Disabled = true
	case field.IsFieldOrTitle("enabled"):
		field.Title = "启用"
	case field.IsFieldOrTitle("isfirst"):
		field.Title = "首位管理员"
		field.Disabled = true
	}
}
