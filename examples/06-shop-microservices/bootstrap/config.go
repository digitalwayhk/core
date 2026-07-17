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

// LocalServiceConfig creates the all-in-one configuration. Discovery remains
// in-process while Redis Streams continues to carry EventBridge messages.
func LocalServiceConfig(name string, port, dataCenterID, machineID int) *config.ServerConfig {
	cfg := baseServiceConfig(name, port, dataCenterID, machineID)
	cfg.Cluster.Mode = "on"
	cfg.Cluster.Provider = "local"
	cfg.Transport.GRPC.Security = config.GRPCSecurityConfig{Mode: "insecure"}
	cfg.ApplyDefaults()
	return cfg
}

// DistributedServiceConfig creates the Redis-discovered, application-mTLS
// configuration used by the three independent service processes.
func DistributedServiceConfig(name string, port, dataCenterID, machineID int) *config.ServerConfig {
	cfg := baseServiceConfig(name, port, dataCenterID, machineID)
	cfg.Cluster.Mode = "on"
	cfg.Cluster.Provider = "redis"
	cfg.Cluster.AdvertiseAddress = AdvertiseAddress()
	cfg.Cluster.HeartbeatInterval = time.Second
	cfg.Cluster.Providers.Redis = config.RedisProviderConfig{Addr: RedisAddress(), Prefix: "core:discovery", TTL: 5 * time.Second}
	cfg.Transport.GRPC.Security = config.GRPCSecurityConfig{
		Mode:       "mtls",
		CAFile:     strings.TrimSpace(os.Getenv("SHOP_GRPC_CA_FILE")),
		CertFile:   strings.TrimSpace(os.Getenv("SHOP_GRPC_CERT_FILE")),
		KeyFile:    strings.TrimSpace(os.Getenv("SHOP_GRPC_KEY_FILE")),
		ServerName: strings.TrimSpace(os.Getenv("SHOP_GRPC_SERVER_NAME")),
	}
	cfg.ApplyDefaults()
	return cfg
}

func baseServiceConfig(name string, port, dataCenterID, machineID int) *config.ServerConfig {
	cfg := config.NewServiceDefaultConfig(name, port)
	cfg.RunIp = "127.0.0.1"
	cfg.DataCenterID = uint(dataCenterID)
	cfg.MachineID = uint(machineID)
	cfg.MQ.Mode = "on"
	cfg.MQ.Provider = "redis-stream"
	cfg.MQ.Usage = []string{"event-stream"}
	cfg.MQ.RedisStream = config.RedisStreamMQConfig{Addr: RedisAddress(), Prefix: "core:event"}
	cfg.Transport.Internal = "grpc"
	cfg.Transport.Fallback = nil
	cfg.Transport.MaxRetries = 1
	cfg.Transport.RetryDelay = 0
	return cfg
}
