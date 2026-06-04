package main

import (
	"encoding/json"
	"fmt"

	"github.com/digitalwayhk/core/pkg/server/config"
)

func main() {
	// 模拟旧配置：只知道服务名和端口，新增字段全部为空。
	cfg := config.NewServiceDefaultConfig("configdemo", 18088)

	// 对子配置显式调用 ApplyDefaults，演示读取旧配置时应补齐默认值。
	cfg.Cluster.ApplyDefaults()
	cfg.Transport.ApplyDefaults()
	cfg.MQ.ApplyDefaults()

	// Validate 用于启动前发现硬错误。例如 MQ.Mode=on 但 NATS URL 为空。
	if err := cfg.Cluster.Validate(); err != nil {
		panic(err)
	}
	if err := cfg.Transport.Validate(); err != nil {
		panic(err)
	}
	if err := cfg.MQ.Validate(); err != nil {
		panic(err)
	}

	data, _ := json.MarshalIndent(map[string]interface{}{
		"cluster":   cfg.Cluster,
		"transport": cfg.Transport,
		"mq":        cfg.MQ,
	}, "", "  ")
	fmt.Println(string(data))
}
