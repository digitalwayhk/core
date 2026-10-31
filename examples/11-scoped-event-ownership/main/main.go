// 本文件只组装标准Core服务；prepare是显式维护命令，不能与运行中的旧消费者同时执行。
package main

import (
	"context"
	"encoding/json"
	demo "github.com/digitalwayhk/core/examples/11-scoped-event-ownership"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/business"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/contract"
	"github.com/digitalwayhk/core/examples/11-scoped-event-ownership/models"
	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/run"
	"github.com/digitalwayhk/core/pkg/server/types"
	"os"
	"strconv"
	"time"
)

func main() {
	role := os.Getenv("CORE_DEMO11_ROLE")
	if role != "prepare" && role != "worker" && role != "gateway" {
		panic("CORE_DEMO11_ROLE必须为prepare/worker/gateway")
	}
	addr, prefix := os.Getenv("CORE_DEMO11_REDIS_ADDR"), os.Getenv("CORE_DEMO11_PREFIX")
	if addr == "" || prefix == "" {
		panic("必须显式提供演示Redis地址及隔离前缀")
	}
	if role == "prepare" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		p := mq.NewRedisStreamProvider(addr, prefix, 0)
		if p.Connect(ctx) != nil {
			panic("无法连接显式演示Redis")
		}
		defer p.Close()
		if err := p.ProvisionOwnership(ctx, contract.Manifest(), 0); err != nil {
			panic(err)
		}
		if err := p.BindOwnership(ctx, contract.Manifest()); err != nil {
			panic(err)
		}
		return
	}
	index, err := strconv.Atoi(os.Getenv("CORE_DEMO11_INDEX"))
	if err != nil || index < 1 || index > 31 {
		panic("演示实例编号必须为1..31且不重复")
	}
	raw, err := os.ReadFile(os.Getenv("CORE_DEMO11_MYSQL_CONFIG_FILE"))
	if err != nil {
		panic("需要显式MySQL配置文件")
	}
	var sql oltp.Config
	if json.Unmarshal(raw, &sql) != nil {
		panic("MySQL配置无效")
	}
	sql.Database = os.Getenv("CORE_DEMO11_DATABASE")
	if sql.Database == "" {
		panic("必须显式提供共享演示数据库名")
	}
	maxOwned := 1
	if text := os.Getenv("CORE_DEMO11_MAX_OWNED"); text != "" {
		maxOwned, err = strconv.Atoi(text)
		if err != nil || maxOwned < 1 || maxOwned > 64 {
			panic("归属容量必须为1..64")
		}
	}
	s := &demo.Service{Runtime: business.New(models.NewStore(&sql)), Worker: role == "worker", MaxOwned: maxOwned}
	cfg := config.NewServiceDefaultConfig(contract.ServiceName, 50111)
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
	ca, cert, key := os.Getenv("CORE_DEMO11_CA_FILE"), os.Getenv("CORE_DEMO11_CERT_FILE"), os.Getenv("CORE_DEMO11_KEY_FILE")
	if ca == "" || cert == "" || key == "" {
		panic("内部专用查询需要显式mTLS服务证书")
	}
	cfg.Transport.Internal = "grpc"
	cfg.Transport.Fallback = nil
	cfg.Transport.GRPC.Security = config.GRPCSecurityConfig{Mode: "mtls", CAFile: ca, CertFile: cert, KeyFile: key}
	cfg.ApplyDefaults()
	server := run.NewWebServer()
	sc := router.NewServiceContextWithConfig(s, cfg)
	server.AddServiceContext(sc)
	server.SetOption(s, &types.ServerOption{IsWebSocket: role == "gateway"})
	server.Start()
}
