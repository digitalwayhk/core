// 本文件提供框架生成的标准命令和公共字段的中英默认标题。
// 消费方的 ViewCommandModel / ViewFieldModel / ViewModel 钩子仍可覆盖这里的结果。
package manage

import (
	"strings"

	"github.com/digitalwayhk/core/pkg/server/locale"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// standardCommandTitles 只覆盖框架生成的标准命令，自定义命令保持类型名。
var standardCommandTitles = map[string][2]string{
	"add":     {"新增", "Add"},
	"edit":    {"编辑", "Edit"},
	"remove":  {"删除", "Remove"},
	"submit":  {"提交", "Submit"},
	"release": {"发布", "Release"},
}

// commonFieldTitles 按 Go 字段名匹配框架公共字段。
var commonFieldTitles = map[string][2]string{
	"ID":              {"编号", "ID"},
	"CreatedAt":       {"创建时间", "Created At"},
	"UpdatedAt":       {"更新时间", "Updated At"},
	"CreatedUserName": {"创建人", "Created By"},
	"UpdatedUserName": {"更新人", "Updated By"},
	"TraceID":         {"追踪号", "Trace ID"},
	"Revision":        {"修订号", "Revision"},
}

// standardCommandTitle 返回标准命令在当前语言下的标题，非标准命令返回空串。
func standardCommandTitle(command, current string) string {
	titles, ok := standardCommandTitles[strings.ToLower(command)]
	if !ok {
		return ""
	}
	return locale.Pick(current, titles[0], titles[1])
}

// commonFieldTitle 返回框架公共字段在当前语言下的标题，其它字段返回空串。
func commonFieldTitle(fieldName, current string) string {
	titles, ok := commonFieldTitles[fieldName]
	if !ok {
		return ""
	}
	return locale.Pick(current, titles[0], titles[1])
}

// localeViewTitle 读取 Manage 控制器为当前语言声明的页面标题。
// 只认 ILocaleTitle：ITitle 表示服务的默认中文标题，不代表页面标题。
func localeViewTitle(instance interface{}, current string) string {
	localeTitle, ok := instance.(types.ILocaleTitle)
	if !ok {
		return ""
	}
	return strings.TrimSpace(localeTitle.GetLocaleTitle(current))
}
