// 本文件验证菜单空表初始化只由 MenuManage 的原子同步链路触发。
package manage

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/server/smodels"
	manageservice "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
	"github.com/stretchr/testify/require"
)

func TestShouldBootstrapMenuSearchOnlyForUnfilteredFirstPage(t *testing.T) {
	firstPage := manageservice.NewSearch[smodels.MenuModel](nil)
	firstPage.SearchItem = &view.SearchItem{Page: 1, Size: 10}
	require.True(t, shouldBootstrapMenuSearch(firstPage, &view.TableData{}))

	filtered := manageservice.NewSearch[smodels.MenuModel](nil)
	filtered.SearchItem = &view.SearchItem{Page: 1, Size: 10, WhereList: []*view.SearchWhere{{Name: "Name", Value: "Pricing"}}}
	require.False(t, shouldBootstrapMenuSearch(filtered, &view.TableData{}))

	laterPage := manageservice.NewSearch[smodels.MenuModel](nil)
	laterPage.SearchItem = &view.SearchItem{Page: 2, Size: 10}
	require.False(t, shouldBootstrapMenuSearch(laterPage, &view.TableData{}))
	require.False(t, shouldBootstrapMenuSearch(firstPage, &view.TableData{Total: 1}))
	require.False(t, shouldBootstrapMenuSearch(nil, &view.TableData{}))
	require.False(t, shouldBootstrapMenuSearch(firstPage, nil))
}
