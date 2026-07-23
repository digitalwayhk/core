// Package bootstrap 组装 07 订单水平扩展示例的发现、事件和内部传输配置。
package bootstrap

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
)

// RedisAddress 返回配置的 Redis 地址。
func RedisAddress() string {
	if value := strings.TrimSpace(os.Getenv("SHOP_REDIS_ADDR")); value != "" {
		return value
	}
	return "127.0.0.1:6379"
}

// OrderHTTPPort 返回 order 进程 HTTP 监听端口。
func OrderHTTPPort() int {
	return envInt("SHOP_ORDER_HTTP_PORT", 18183)
}

// OrderGRPCPort 返回 order 进程内部 gRPC 监听端口。
func OrderGRPCPort(httpPort int) int {
	return envInt("SHOP_ORDER_GRPC_PORT", httpPort+10000)
}

// AdvertiseAddress 返回服务注册到发现中心的地址。
func AdvertiseAddress() string {
	if value := strings.TrimSpace(os.Getenv("SHOP_ADVERTISE_ADDRESS")); value != "" {
		return value
	}
	if hostname, err := os.Hostname(); err == nil && strings.TrimSpace(hostname) != "" {
		return strings.TrimSpace(hostname)
	}
	return "127.0.0.1"
}

// LocalServiceConfig 创建 all-in-one 调试配置。
func LocalServiceConfig(name string, port, dataCenterID, machineID int) *config.ServerConfig {
	cfg := baseServiceConfig(name, port, dataCenterID, machineID)
	cfg.Cluster.Mode = "on"
	cfg.Cluster.Provider = "local"
	cfg.Transport.GRPC.Security = config.GRPCSecurityConfig{Mode: "insecure"}
	cfg.ApplyDefaults()
	return cfg
}

// DistributedServiceConfig 创建独立进程配置。
func DistributedServiceConfig(name string, port, dataCenterID, machineID int) *config.ServerConfig {
	cfg := baseServiceConfig(name, port, dataCenterID, machineID)
	cfg.Cluster.Mode = "on"
	cfg.Cluster.Provider = "redis"
	cfg.Cluster.AdvertiseAddress = AdvertiseAddress()
	cfg.Cluster.HeartbeatInterval = time.Second
	cfg.Cluster.Providers.Redis = config.RedisProviderConfig{Addr: RedisAddress(), Prefix: "core:discovery:07", TTL: 5 * time.Second}
	cfg.Transport.GRPC.Security = config.GRPCSecurityConfig{
		Mode:       envString("SHOP_GRPC_SECURITY_MODE", "mesh"),
		CAFile:     strings.TrimSpace(os.Getenv("SHOP_GRPC_CA_FILE")),
		CertFile:   strings.TrimSpace(os.Getenv("SHOP_GRPC_CERT_FILE")),
		KeyFile:    strings.TrimSpace(os.Getenv("SHOP_GRPC_KEY_FILE")),
		ServerName: envString("SHOP_GRPC_SERVER_NAME", config.GRPCServerNameTargetService),
	}
	cfg.ApplyDefaults()
	return cfg
}

// DistributedOrderConfig 创建可水平扩展 order 副本配置。
func DistributedOrderConfig(port, dataCenterID int) *config.ServerConfig {
	cfg := DistributedServiceConfig("shop-order", port, dataCenterID, 0)
	cfg.Transport.GRPC.Port = OrderGRPCPort(port)
	cfg.Cluster.Claim.AutoMachineID = true
	cfg.Cluster.Claim.MachineIDMax = 31
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
	cfg.MQ.RedisStream = config.RedisStreamMQConfig{Addr: RedisAddress(), Prefix: "core:event:07"}
	cfg.Transport.Internal = "grpc"
	cfg.Transport.Fallback = nil
	cfg.Transport.MaxRetries = 1
	cfg.Transport.RetryDelay = 0
	return cfg
}

func envInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func envString(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}
