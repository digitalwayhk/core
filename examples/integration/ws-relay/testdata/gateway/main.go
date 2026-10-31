// 本文件仅为跨进程WS回归启动隔离的真实Core Gateway，不连接业务环境。
package main

import (
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/contract"
	"github.com/digitalwayhk/core/pkg/server/api/manage"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/run"
	"github.com/digitalwayhk/core/pkg/server/types"
	"os"
	"strconv"
	"time"
)

type gateway struct{}

// ServiceName 返回两个Gateway共用的逻辑服务名。
func (*gateway) ServiceName() string { return contract.ServiceName }

// Routers 只注册测试业务路由，中继另走标准ServerManage注册。
func (*gateway) Routers() []types.IRouter {
	return []types.IRouter{&privateView{}, &publicView{}, &ownedView{}, &control{}}
}
func main() {
	index, err := strconv.Atoi(os.Getenv("CORE_RELAY_INDEX"))
	if err != nil {
		panic(err)
	}
	addr, prefix := os.Getenv("CORE_TEST_REDIS_ADDR"), os.Getenv("CORE_RELAY_PREFIX")
	if addr == "" || prefix == "" {
		panic("必须显式提供隔离Redis")
	}
	cfg := config.NewServiceDefaultConfig(contract.ServiceName, 50147)
	cfg.Host = "127.0.0.1"
	cfg.DataCenterID = 2
	cfg.MachineID = uint(index)
	cfg.Cluster.Mode = "on"
	cfg.Cluster.Provider = "redis"
	cfg.Cluster.AdvertiseAddress = "127.0.0.1"
	cfg.Cluster.Providers.Redis = config.RedisProviderConfig{Addr: addr, Prefix: prefix + ":discovery", TTL: 5 * time.Second}
	cfg.Cluster.HeartbeatInterval = time.Second
	cfg.MQ.Mode = "on"
	cfg.MQ.Provider = "redis-stream"
	cfg.MQ.Usage = []string{"event-stream"}
	cfg.MQ.RedisStream = config.RedisStreamMQConfig{Addr: addr, Prefix: prefix}
	cfg.Transport.Internal = "grpc"
	cfg.Transport.Fallback = nil
	cfg.Transport.GRPC.Security = config.GRPCSecurityConfig{Mode: "mtls", CAFile: os.Getenv("CORE_RELAY_CA"), CertFile: os.Getenv("CORE_RELAY_CERT"), KeyFile: os.Getenv("CORE_RELAY_KEY")}
	cfg.ApplyDefaults()
	s := &gateway{}
	server := run.NewWebServer()
	sc := router.NewServiceContextWithConfig(s, cfg)
	server.AddServiceContext(sc)
	sc.Router.AddServerRouters(&manage.SyncSubscription{}, &manage.ForwardNotice{})
	server.SetOption(s, &types.ServerOption{IsWebSocket: true})
	server.Start()
}
