package hooks

import (
	"github.com/digitalwayhk/core/examples/04-manage-hooks/api/manage"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// HookService 注册一个带 hook 的管理服务。
type HookService struct{}

func (own *HookService) ServiceName() string { return "hooks" }

func (own *HookService) Routers() []types.IRouter {
	return manage.NewArticleManage().Routers()
}

func (own *HookService) SubscribeRouters() []*types.ObserveArgs { return nil }
