package cluster

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func BenchmarkLocalProviderListRunningNodes(b *testing.B) {
	provider := NewLocalProvider(time.Minute, time.Minute, time.Minute)
	for i := 0; i < 100; i++ {
		if err := provider.Register(context.Background(), &NodeInfo{
			ID:           fmt.Sprintf("node-%d", i),
			ServiceName:  "orders",
			DataCenterID: 1,
			MachineID:    int64(i),
			Status:       NodeStatusRunning,
		}); err != nil {
			b.Fatal(err)
		}
	}

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			nodes, err := provider.List(context.Background(), "orders", NodeStatusRunning)
			if err != nil || len(nodes) != 100 {
				b.Fatalf("List 失败: nodes=%d err=%v", len(nodes), err)
			}
		}
	})
}
