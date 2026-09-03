package smodels

import "github.com/digitalwayhk/core/pkg/persistence/entity/stats"

// MenuServiceSnapshot 是单个服务进程对 UpdateMenu 暴露的菜单发现快照。
//
// Name 是稳定服务名；Routers 和 Reports 均由目标服务自己的注册表生成，避免管理入口
// 进程通过本地 router registry 猜测其它容器的路由和报表。
type MenuServiceSnapshot struct {
	Name    string                 `json:"name"`
	Title   string                 `json:"title"`
	TitleEN string                 `json:"titleEN"`
	Routers []MenuRouterSnapshot   `json:"routers"`
	Reports []stats.ReportMenuItem `json:"reports"`
}
