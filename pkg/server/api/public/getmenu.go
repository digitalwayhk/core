package public

import (
	"net/http"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	pt "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/api"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type GetMenu struct {
}

func (own *GetMenu) Parse(req types.IRequest) error {
	return nil
}
func (own *GetMenu) Validation(req types.IRequest) error {
	return nil
}
func (own *GetMenu) Do(req types.IRequest) (interface{}, error) {
	list := entity.NewModelList[smodels.DirectoryModel](nil)
	dirs, _, err := list.SearchAll(1, 1000, func(item *pt.SearchItem) {
		item.AddSortN("Sort", false)
	})
	if err != nil {
		return nil, err
	}
	if len(dirs) > 0 {
		for _, dir := range dirs {
			list := entity.NewModelList[smodels.MenuModel](nil)
			rows, _, err := list.SearchAll(1, 1000, func(item *pt.SearchItem) {
				item.AddWhereN("DirectoryModelID", dir.ID)
				item.AddSortN("Sort", false)
			})
			if err != nil {
				return nil, err
			}
			dir.MenuItems = rows
		}
	}
	return dirs, nil
}

func (own *GetMenu) RouterInfo() *types.RouterInfo {
	info := api.ServerRouterInfo(own)
	info.Method = http.MethodGet
	return info
}
