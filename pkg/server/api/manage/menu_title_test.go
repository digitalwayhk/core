// 本文件验证目录与菜单中英标题的推导来源、回退顺序和目录合并规则。
package manage

import (
	"testing"

	"github.com/digitalwayhk/core/pkg/server/locale"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/stretchr/testify/require"
)

// localeTitleManage 模拟实现 ILocaleTitle 的消费方 Manage 控制器。
type localeTitleManage struct{}

func (own *localeTitleManage) GetLocaleTitle(current string) string {
	if current == locale.EnUS {
		return "Tokens"
	}
	return "代币管理"
}

// titleOnlyManage 模拟只实现 ITitle 的旧服务或控制器。
type titleOnlyManage struct{}

func (own *titleOnlyManage) GetTitle() string { return "订单服务" }

// plainManage 模拟两个标题接口都没实现的控制器。
type plainManage struct{}

// packRouter 模拟 View[T]/Search[T] 这类包装 Router，真实控制器藏在 GetInstance 后面。
type packRouter struct {
	inner interface{}
}

func (own *packRouter) Parse(req types.IRequest) error             { return nil }
func (own *packRouter) Validation(req types.IRequest) error        { return nil }
func (own *packRouter) Do(req types.IRequest) (interface{}, error) { return nil, nil }
func (own *packRouter) RouterInfo() *types.RouterInfo              { return nil }
func (own *packRouter) GetInstance() interface{}                   { return own.inner }

func routerInfoWith(t *testing.T, instance types.IRouter) *types.RouterInfo {
	t.Helper()
	info := &types.RouterInfo{}
	info.SetInstance(instance)
	return info
}

// 标题声明在被包装的 Manage 控制器上，不能停在 View/Search 操作对象。
func TestManageOwnerUnwrapsPackRouterHook(t *testing.T) {
	inner := &localeTitleManage{}
	info := routerInfoWith(t, &packRouter{inner: inner})
	require.Same(t, inner, manageOwner(info))
}

func TestManageOwnerKeepsInstanceWithoutHook(t *testing.T) {
	wrapper := &packRouter{inner: nil}
	info := routerInfoWith(t, wrapper)
	require.Same(t, wrapper, manageOwner(info))
	require.Nil(t, manageOwner(nil))
}

func TestLocaleTitlesReadsBothLanguages(t *testing.T) {
	title, titleEN := localeTitles(&localeTitleManage{}, "TokenManage")
	require.Equal(t, "代币管理", title)
	require.Equal(t, "Tokens", titleEN)
}

// 只实现 ITitle 时英文列留空，由 getmenu 回退中文，行为与本能力之前一致。
func TestLocaleTitlesLeavesEnglishEmptyForLegacyInstances(t *testing.T) {
	title, titleEN := localeTitles(&titleOnlyManage{}, "OrderManage")
	require.Equal(t, "订单服务", title)
	require.Equal(t, "", titleEN)
}

func TestLocaleTitlesFallsBackToInstanceName(t *testing.T) {
	title, titleEN := localeTitles(&plainManage{}, "TokenManage")
	require.Equal(t, "TokenManage", title)
	require.Equal(t, "", titleEN)

	title, titleEN = localeTitles(&plainManage{}, "")
	require.Equal(t, "", title)
	require.Equal(t, "", titleEN)
}

func TestDirectoryTitleMergeFollowsMenuRules(t *testing.T) {
	withTitles := func(title, titleEN string) *smodels.DirectoryModel {
		directory := smodels.NewDirectoryModel()
		directory.Title = title
		directory.TitleEN = titleEN
		return directory
	}

	require.False(t, directoryTitlesChanged(withTitles("演示服务", "Demo"), withTitles("演示服务", "Demo")))
	require.True(t, directoryTitlesChanged(withTitles("演示服务", ""), withTitles("演示服务", "Demo")))
	require.False(t, directoryTitlesChanged(withTitles("演示服务", ""), withTitles("", "")))

	old := withTitles("旧目录", "Stale")
	old.Sort = 5
	old.Icon = "folder"
	merged := mergeGeneratedDirectory(old, withTitles("演示服务", "Demo"))
	require.Equal(t, "演示服务", merged.Title)
	require.Equal(t, "Demo", merged.TitleEN)
	require.Equal(t, 5, merged.Sort)
	require.Equal(t, "folder", merged.Icon)
}
