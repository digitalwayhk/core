// Package bootstrap 组装示例 06 的 Redis、服务发现、事件和内部传输配置。
package bootstrap

import (
	"os"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
)

func RedisAddress() string {
	if value := strings.TrimSpace(os.Getenv("SHOP_REDIS_ADDR")); value != "" {
		return value
	}
	return "127.0.0.1:6379"
}
func AdvertiseAddress() string {
	if value := strings.TrimSpace(os.Getenv("SHOP_ADVERTISE_ADDRESS")); value != "" {
		return value
	}
	return "127.0.0.1"
}

// ServiceConfig 创建一个独立服务的显式运行配置。
func ServiceConfig(name string, port, dataCenterID, machineID int) *config.ServerConfig {
	cfg := config.NewServiceDefaultConfig(name, port)
	cfg.RunIp = "127.0.0.1"
	cfg.DataCenterID = uint(dataCenterID)
	cfg.MachineID = uint(machineID)
	cfg.SocketPort = port + 10000
	cfg.Cluster.Mode = "on"
	cfg.Cluster.Provider = "redis"
	cfg.Cluster.AdvertiseAddress = AdvertiseAddress()
	cfg.Cluster.HeartbeatInterval = time.Second
	cfg.Cluster.Providers.Redis = config.RedisProviderConfig{Addr: RedisAddress(), Prefix: "core:discovery", TTL: 5 * time.Second}
	cfg.MQ.Mode = "on"
	cfg.MQ.Provider = "redis-stream"
	cfg.MQ.Usage = []string{"event-stream"}
	cfg.MQ.RedisStream = config.RedisStreamMQConfig{Addr: RedisAddress(), Prefix: "core:event"}
	cfg.Transport.Internal = "socket"
	cfg.Transport.Fallback = []string{"socket"}
	cfg.Transport.MaxRetries = 1
	cfg.Transport.RetryDelay = 0
	cfg.ApplyDefaults()
	return cfg
}
