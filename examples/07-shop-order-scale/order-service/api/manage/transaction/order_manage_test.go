// 本文件验证 07 订单管理查询沿用 Core 标准 ModelList 查询链路，并绑定远程权威库。
package transaction

import (
	"testing"

	"github.com/digitalwayhk/core/examples/07-shop-order-scale/order-service/models"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	managepkg "github.com/digitalwayhk/core/service/manage"
	"github.com/digitalwayhk/core/service/manage/view"
	"github.com/stretchr/testify/require"
)

func TestOrderManageUsesRemoteModelList(t *testing.T) {
	manager := NewOrderManage()
	list, ok := manager.GetList().(*entity.ModelList[models.Order])

	require.True(t, ok)
	require.Same(t, models.RemoteDataAction(), list.GetAction())
}

func TestOrderManageDoesNotInterceptStandardSearch(t *testing.T) {
	manager := NewOrderManage()
	search := &managepkg.Search[models.Order]{
		SearchItem: &view.SearchItem{
			Page: 2,
			Size: 25,
			Tag:  "order-manage",
			WhereList: []*view.SearchWhere{
				{Name: "UserID", Value: float64(12345)},
			},
			SortList: []*view.SearchSort{{Name: "CreatedAt", Isdesc: true}},
		},
	}

	data, err, stop := manager.OnSearchBefore(search, nil)

	require.NoError(t, err)
	require.False(t, stop)
	require.Nil(t, data)
}
