package manage

import (
	"testing"

	"github.com/digitalwayhk/core/service/manage/view"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCategoryManageViewFieldKind 验证分类类型字段配置了 Comvtp。
func TestCategoryManageViewFieldKind(t *testing.T) {
	manager := NewCategoryManage()
	field := &view.FieldModel{Field: "kind", PropField: "Kind", Title: "Kind"}
	manager.ViewFieldModel(nil, field)
	require.NotNil(t, field.ComVtp)
	assert.True(t, field.ShowInComvtp)
	assert.Equal(t, "电子", field.ComVtp.Items[0])
	assert.Equal(t, "图书", field.ComVtp.Items[1])
	assert.Equal(t, "配件", field.ComVtp.Items[2])
}

// TestCatalogItemManageViewCommands 验证导入导出不要求选中行，复制打开表单。
func TestCatalogItemManageViewCommands(t *testing.T) {
	manager := NewCatalogItemManage()
	importCmd := &view.CommandModel{Command: "importdata"}
	exportCmd := &view.CommandModel{Command: "exportdata"}
	cloneCmd := &view.CommandModel{Command: "cloneitem"}
	manager.ViewCommandModel(importCmd)
	manager.ViewCommandModel(exportCmd)
	manager.ViewCommandModel(cloneCmd)
	assert.False(t, importCmd.IsSelectRow)
	assert.False(t, exportCmd.IsSelectRow)
	assert.True(t, importCmd.IsSplit)
	assert.Equal(t, "add", importCmd.SplitName)
	assert.True(t, exportCmd.IsSplit)
	assert.Equal(t, "add", exportCmd.SplitName)
	assert.True(t, cloneCmd.EditShow)
	assert.True(t, cloneCmd.IsSelectRow)
	assert.True(t, cloneCmd.IsSplit)
	assert.Equal(t, "add", cloneCmd.SplitName)
}

// TestCatalogItemManageViewFieldWidgets 验证密码、备注和外键字段标记。
func TestCatalogItemManageViewFieldWidgets(t *testing.T) {
	manager := NewCatalogItemManage()
	secret := &view.FieldModel{Field: "secret", PropField: "Secret"}
	note := &view.FieldModel{Field: "note", PropField: "Note"}
	price := &view.FieldModel{Field: "price", PropField: "Price"}
	manager.ViewFieldModel(nil, secret)
	manager.ViewFieldModel(nil, note)
	manager.ViewFieldModel(nil, price)
	assert.True(t, secret.IsPassword)
	assert.False(t, secret.IsSearch)
	assert.True(t, note.IsRemark)
	assert.Equal(t, 2, price.Precision)
}

// TestCatalogItemManageViewChildModels 验证条目配置了明细行和规格参数两个子表。
func TestCatalogItemManageViewChildModels(t *testing.T) {
	manager := NewCatalogItemManage()
	lines := &view.ViewChildModel{}
	lines.Name = "Lines"
	lines.Fields = []*view.FieldModel{
		{Field: "itemID", Title: "ItemID"},
		{Field: "lineNo", Title: "LineNo"},
		{Field: "name", Title: "Name"},
	}
	specs := &view.ViewChildModel{}
	specs.Name = "Specs"
	specs.Fields = []*view.FieldModel{
		{Field: "itemID", Title: "ItemID"},
		{Field: "specName", Title: "SpecName"},
		{Field: "specValue", Title: "SpecValue"},
	}
	manager.ViewChildModel(lines)
	manager.ViewChildModel(specs)
	assert.Equal(t, "明细行", lines.Title)
	assert.Equal(t, "ItemID", lines.ForeignKey)
	assert.False(t, lines.Fields[0].Visible)
	assert.Equal(t, "行号", lines.Fields[1].Title)
	assert.Equal(t, "规格参数", specs.Title)
	assert.Equal(t, "ItemID", specs.ForeignKey)
	assert.False(t, specs.Fields[0].Visible)
	assert.Equal(t, "参数名", specs.Fields[1].Title)
}
