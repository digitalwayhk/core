// 本文件启动菜单发现回归专用的真实服务，配置、数据库和发现前缀全部隔离。
package main

import (
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/run"
	"github.com/digitalwayhk/core/pkg/server/types"
)

type menuService struct{}

// ServiceName 返回两个独立进程共用的逻辑服务名。
func (*menuService) ServiceName() string { return "menu-peer" }

// Routers 注册可被菜单发现的隔离测试路由。
func (*menuService) Routers() []types.IRouter { return []types.IRouter{&itemSearch{}} }

type itemSearch struct{}

// Parse 接受无筛选条件的测试查询。
func (*itemSearch) Parse(types.IRequest) error { return nil }

// Validation 交由框架Manage认证域校验。
func (*itemSearch) Validation(types.IRequest) error { return nil }

// Do 返回固定的测试数据，不连接业务存储。
func (*itemSearch) Do(types.IRequest) (interface{}, error) {
	return []string{"isolated-test-item"}, nil
}

// GetTitle 返回菜单元数据需要的中文标题。
func (*itemSearch) GetTitle() string { return "跨进程菜单回归" }

// RouterInfo 显式声明测试Manage路径与逻辑服务归属。
func (r *itemSearch) RouterInfo() *types.RouterInfo {
	return router.NewRouterInfoWithOptions(r, "fixture/api/manage", "ItemManage",
		router.WithServiceName("menu-peer"), router.WithPath("/api/manage/menu-peer/itemmanage/search"),
		router.WithPathType(types.ManageType), router.WithMethod(http.MethodPost), router.WithAuth(true))
}

func main() {
	if err := os.WriteFile("menu-peer.pid", []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		panic(err)
	}
	index, err := strconv.Atoi(os.Getenv("CORE_MENU_INDEX"))
	if err != nil {
		panic(err)
	}
	cfg := config.NewServiceDefaultConfig("menu-peer", 50147)
	cfg.Host, cfg.DataCenterID, cfg.MachineID = "127.0.0.1", 2, uint(index)
	cfg.Cluster.Mode, cfg.Cluster.Provider = "on", "redis"
	cfg.Cluster.AdvertiseAddress = "127.0.0.1"
	cfg.Cluster.Providers.Redis = config.RedisProviderConfig{Addr: os.Getenv("CORE_TEST_REDIS_ADDR"),
		Prefix: os.Getenv("CORE_MENU_PREFIX"), TTL: 5 * time.Second}
	cfg.Cluster.HeartbeatInterval = time.Second
	cfg.MQ.Mode = "off"
	cfg.Transport.Internal, cfg.Transport.Fallback = "grpc", nil
	cfg.Transport.GRPC.Security = config.GRPCSecurityConfig{Mode: "mtls", CAFile: os.Getenv("CORE_MENU_CA"),
		CertFile: os.Getenv("CORE_MENU_CERT"), KeyFile: os.Getenv("CORE_MENU_KEY")}
	cfg.ApplyDefaults()
	server := run.NewWebServer()
	server.AddServiceContext(router.NewServiceContextWithConfig(&menuService{}, cfg))
	server.Start()
}
