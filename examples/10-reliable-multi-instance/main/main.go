package main

import (
	"os"
	"strconv"
	"time"

	demo "github.com/digitalwayhk/core/examples/10-reliable-multi-instance"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/run"
)

func main() {
	role := os.Getenv("CORE_DEMO10_ROLE")
	if role != "producer" && role != "worker" {
		panic("CORE_DEMO10_ROLE 必须为 producer 或 worker")
	}
	index, err := strconv.Atoi(os.Getenv("CORE_DEMO10_INDEX"))
	if err != nil || index < 1 || index > 31 {
		panic("CORE_DEMO10_INDEX 必须为 1..31 的唯一实例编号")
	}
	addr := os.Getenv("CORE_DEMO10_REDIS_ADDR")
	if addr == "" {
		panic("必须显式设置 CORE_DEMO10_REDIS_ADDR，不探测默认 Redis")
	}
	prefix := os.Getenv("CORE_DEMO10_PREFIX")
	if prefix == "" {
		panic("必须显式设置 CORE_DEMO10_PREFIX，隔离不同演示")
	}
	s := &demo.Service{Worker: role == "worker"}
	cfg := config.NewServiceDefaultConfig(s.ServiceName(), 50101)
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
	cfg.MQ.RedisStream = config.RedisStreamMQConfig{Addr: addr, Prefix: prefix + ":events"}
	cfg.Transport.Internal = "grpc"
	cfg.Transport.Fallback = nil
	cfg.Transport.GRPC.Security = config.GRPCSecurityConfig{Mode: "insecure"}
	cfg.ApplyDefaults()
	server := run.NewWebServer()
	server.AddServiceContext(router.NewServiceContextWithConfig(s, cfg))
	server.Start()
}
