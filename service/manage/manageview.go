package manage

import (
	"strings"

	"github.com/digitalwayhk/core/pkg/server/locale"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/digitalwayhk/core/pkg/utils"
	"github.com/digitalwayhk/core/service/manage/view"
)

func GetViewModel(instance interface{}) *view.ViewModel {
	if instance == nil {
		return nil
	}
	vm := &view.ViewModel{}
	vm.Name = utils.GetTypeName(instance)
	vm.Title = vm.Name
	vm.Commands = make([]*view.CommandModel, 0)
	vm.Fields = make([]*view.FieldModel, 0)
	if mv, ok := instance.(IManageView); ok {
		if ms, ok := instance.(IManageService); ok {
			for index, router := range ms.Routers() {
				info := router.RouterInfo()
				vm.ServiceName = info.GetServiceName()
				cmd := RouterToCommand(info)
				if cmd != nil {
					cmd.Index = index
					if mv != nil {
						mv.ViewCommandModel(cmd)
					}
					vm.Commands = append(vm.Commands, cmd)
				}
			}
		}
	}
	return vm
}

// RouterToCommand 按默认语言生成命令模型，签名保持不变供既有消费方调用。
func RouterToCommand(info *types.RouterInfo) *view.CommandModel {
	return RouterToLocaleCommand(info, locale.Default)
}

// RouterToLocaleCommand 生成命令模型并按当前语言填写标准命令标题。
// Command、Name 是稳定键，不随语言变；只有 Title 是展示文案。
func RouterToLocaleCommand(info *types.RouterInfo, current string) *view.CommandModel {
	if info == nil {
		return nil
	}
	structName := info.GetStructName()
	count := strings.Index(structName, "[")
	name := structName
	if count > 0 {
		name = structName[0:count]
	}
	if name == "View" || name == "Search" {
		return nil
	}
	cmd := &view.CommandModel{
		Command: strings.ToLower(name),
		Name:    name,
		Title:   name,
	}
	if title := standardCommandTitle(cmd.Command, current); title != "" {
		cmd.Title = title
	}
	if cmd.Command != "add" {
		cmd.IsSelectRow = true
	}
	if cmd.Command != "add" && cmd.Command != "edit" {
		cmd.IsAlert = true
	}
	// if cmd.Name == "Release" {
	// 	cmd.IsSplit = true
	// 	cmd.SplitName = "submit"
	// }
	return cmd
}
