package public

import (
	"errors"
	"sort"
	"strconv"

	"github.com/digitalwayhk/core/pkg/persistence/entity/stats"
	"github.com/digitalwayhk/core/pkg/server/api"
	"github.com/digitalwayhk/core/pkg/server/locale"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/smodels"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type QueryRouters struct {
	api.ServerArgs
	ApiType int  `json:"apiType"`
	ForMenu bool `json:"forMenu"`
}

func (own *QueryRouters) Parse(req types.IRequest) error {
	if httpReq, ok := req.(types.IRequestHttp); ok && httpReq.GetHttpRequest() != nil {
		if err := req.Bind(own); err != nil {
			return err
		}
	}
	ats := req.GetValue("apitype")
	if ats != "" {
		at, err := strconv.Atoi(ats)
		if err != nil {
			return err
		}
		own.ApiType = at
	}
	if value := req.GetValue("formenu"); value != "" {
		forMenu, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		own.ForMenu = forMenu
	}
	return nil
}

func (own *QueryRouters) Validation(req types.IRequest) error {
	if own.ForMenu {
		if callerReq, ok := req.(types.IRequestInternalCaller); ok {
			if _, trusted := callerReq.TrustedInternalCaller(); trusted {
				return nil
			}
		}
	}
	return own.ServerArgs.Validation(req)
}
func (own *QueryRouters) Do(req types.IRequest) (interface{}, error) {
	sc := router.GetContext(req.ServiceName())
	if sc == nil || sc.Router == nil {
		return nil, errors.New("query routers service context unavailable")
	}
	apitype := ""
	if own.ApiType == 1 {
		apitype = string(types.PublicType)
	}
	if own.ApiType == 2 {
		apitype = string(types.PrivateType)
	}
	if own.ApiType == 3 {
		apitype = string(types.ManageType)
	}
	var routers []*types.RouterInfo
	if apitype == "" {
		routers = sc.Router.GetRouters()
	} else {
		routers = sc.Router.GetTypeRouters(types.ApiType(apitype))
	}
	if own.ForMenu {
		return NewMenuServiceSnapshot(sc), nil
	}
	return routers, nil
}

func (own *QueryRouters) RouterInfo() *types.RouterInfo {
	return api.ServerRouterInfoWithOptions(own, withSystemEndpointRateLimit())
}

// NewMenuServiceSnapshot 从目标服务自己的进程内注册表生成菜单发现快照。
//
// 调用方可以跨服务传输该快照，但不得把其中的数据写回或冒充目标 RouterInfo。
func NewMenuServiceSnapshot(sc *router.ServiceContext) *smodels.MenuServiceSnapshot {
	snapshot := &smodels.MenuServiceSnapshot{
		Routers: []smodels.MenuRouterSnapshot{},
		Reports: []stats.ReportMenuItem{},
	}
	if sc == nil || sc.Service == nil {
		return snapshot
	}
	snapshot.Name = sc.Service.Name
	snapshot.Title = locale.Title(sc.Service.Instance, locale.ZhCN)
	if snapshot.Title == "" {
		snapshot.Title = snapshot.Name
	}
	snapshot.TitleEN = locale.Title(sc.Service.Instance, locale.EnUS)
	if sc.Router == nil {
		return snapshot
	}
	for _, info := range sc.Router.GetTypeRouters(types.ManageType) {
		if info == nil || info.GetPath() == "" {
			continue
		}
		instance := info.GetInstance()
		if hook, ok := instance.(types.IPackRouterHook); ok && hook.GetInstance() != nil {
			instance = hook.GetInstance()
		}
		title := locale.Title(instance, locale.ZhCN)
		if title == "" {
			title = info.GetInstanceName()
		}
		snapshot.Routers = append(snapshot.Routers, smodels.MenuRouterSnapshot{
			Path:         info.GetPath(),
			InstanceName: info.GetInstanceName(),
			Title:        title,
			TitleEN:      locale.Title(instance, locale.EnUS),
		})
	}
	sort.Slice(snapshot.Routers, func(i, j int) bool {
		return snapshot.Routers[i].Path < snapshot.Routers[j].Path
	})
	snapshot.Reports = stats.ListReportMenus(snapshot.Name)
	return snapshot
}
