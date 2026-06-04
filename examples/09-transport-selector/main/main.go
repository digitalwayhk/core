package main

import (
	"fmt"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/transport"
)

func main() {
	// 正常配置：gRPC 优先，HTTP 和 Socket 作为 fallback。
	cfg := config.TransportConfig{
		Internal: "grpc",
		Fallback: []string{"http", "socket"},
	}
	cfg.ApplyDefaults()

	selector, err := transport.BuildSelector(cfg)
	if err != nil {
		panic(err)
	}
	fmt.Printf("selector 已创建：%T\n", selector)

	// 错误配置：mq 目前是消息队列 provider，不是已实现的 direct transport。
	_, err = transport.BuildSelector(config.TransportConfig{Internal: "mq"})
	fmt.Printf("Internal=mq 的预期错误：%v\n", err)
}
