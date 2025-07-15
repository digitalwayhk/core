package manage

import (
	"strings"

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
				vm.ServiceName = info.ServiceName
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
func RouterToCommand(info *types.RouterInfo) *view.CommandModel {
	count := strings.Index(info.StructName, "[")
	name := info.StructName
	if count > 0 {
		name = info.StructName[0:count]
	}
	if name == "View" || name == "Search" {
		return nil
	}
	cmd := &view.CommandModel{
		Command: strings.ToLower(name),
		Name:    name,
		Title:   name,
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
