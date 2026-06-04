package main

import (
	"context"
	"fmt"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
)

func main() {
	ctx := context.Background()

	// LocalProvider 是进程内注册发现实现，适合开发、单机和默认自研模式。
	provider := cluster.NewLocalProvider(3*time.Second, 10*time.Second, 30*time.Second)
	provider.Start()
	defer provider.Close()

	node := &cluster.NodeInfo{
		ID:            "funds-dc1-machine1",
		ServiceName:   "funds",
		DataCenterID:  1,
		MachineID:     1,
		Address:       "127.0.0.1",
		Port:          18090,
		Status:        cluster.NodeStatusRunning,
		LastHeartbeat: time.Now(),
	}

	if err := provider.Register(ctx, node); err != nil {
		panic(err)
	}

	nodes, err := provider.List(ctx, "funds", cluster.NodeStatusRunning)
	if err != nil {
		panic(err)
	}
	fmt.Printf("funds 当前运行实例数：%d\n", len(nodes))

	// 服务停止时主动下线，避免后续请求继续命中该实例。
	if err := provider.Deregister(ctx, node.ID); err != nil {
		panic(err)
	}
}
