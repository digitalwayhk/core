// 本文件验证 07 商品管理视图将供应商字段声明为可搜索选择的外键。
package basedata

import (
	"testing"

	"github.com/digitalwayhk/core/service/manage/view"
	"github.com/stretchr/testify/require"
)

func TestProductManageSupplierIsForeignSelection(t *testing.T) {
	manager := NewProductManage()

	result, err := manager.View.Do(nil)

	require.NoError(t, err)
	model, ok := result.(*view.ViewModel)
	require.True(t, ok)
	field := model.ViewField("SupplierID")
	require.NotNil(t, field)
	require.True(t, field.Required)
	require.Equal(t, "供应商", field.Title)
	require.NotNil(t, field.Foreign)
	require.True(t, field.Foreign.IsFkey)
	require.Equal(t, "Supplier", field.Foreign.OneObjectTypeName)
	require.Equal(t, "name", field.Foreign.OneDisplayName)
}
