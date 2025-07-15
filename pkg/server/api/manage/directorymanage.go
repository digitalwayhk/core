package manage

import (
	"fmt"

	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/server/api/public"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/zeromicro/go-zero/core/logx"
)

type DirectoryManage struct {
	*DmpBase[smodels.DirectoryModel]
}

func NewDirectoryManage() *DirectoryManage {
	own := &DirectoryManage{}
	own.DmpBase = NewDmpBase[smodels.DirectoryModel](own)
	return own
}
func (own *DirectoryManage) Routers() []types.IRouter {
	routers := own.DmpBase.Routers()
	routers = append(routers, own.Add)
	routers = append(routers, own.Remove)
	return routers
}

func (own *DirectoryManage) GetDefaultItems() []*smodels.DirectoryModel {
	qr := &public.QueryRouters{}
	info := qr.RouterInfo()
	path := info.Path + "/%s" + "?apitype=3"
	items := make([]*smodels.DirectoryModel, 0)
	for _, sc := range router.GetContexts() {
		if sc.Service.Name == "server" {
			continue // 排除 server 服务
		}
		item := smodels.NewDirectoryModel()
		item.Name = sc.Service.Name
		item.Url = fmt.Sprintf(path, sc.Service.Name)
		if ititle, ok := sc.Service.Instance.(types.ITitle); ok {
			item.Title = ititle.GetTitle()
		}
		items = append(items, item)
	}
	return items

}

func (own *DirectoryManage) GetDirectoryModel() []*smodels.DirectoryModel {
	if list := own.GetList().(*entity.ModelList[smodels.DirectoryModel]); list != nil {
		rows, _, err := list.SearchAll(1, 1000)
		if err != nil {
			logx.Errorf("GetDirectoryModel error: %v", err)
			return nil
		}
		return rows
	}
	return nil
}
