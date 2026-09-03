package smodels

// MenuRouterSnapshot 是远程菜单发现所需的最小 Manage 路由快照。
//
// 它不携带 RouterInfo 的进程内实例和运行状态，只传递生成菜单与权限所需的稳定字段。
type MenuRouterSnapshot struct {
	Path         string `json:"path"`
	InstanceName string `json:"instanceName"`
	Title        string `json:"title"`
	TitleEN      string `json:"titleEN"`
}
