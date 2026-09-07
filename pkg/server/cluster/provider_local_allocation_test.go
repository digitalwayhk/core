package cluster

// 本文件验证本地 MachineID 分配遵守注册冷却规则，不依赖等待或后台时钟推进。
import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestLocalProvider_AllocateMachineID_Cooldown 验证冷却占用、耗尽、到期重用及服务和数据中心隔离。
func TestLocalProvider_AllocateMachineID_Cooldown(t *testing.T) {
	for _, tc := range []struct {
		name     string
		age      time.Duration
		service  string
		dc       int64
		fillNext bool
		want     int64
	}{
		{"冷却中跳过", 0, "orders", 0, false, 1},
		{"冷却与运行占满", 0, "orders", 0, true, -1},
		{"冷却到期重用", 2 * time.Hour, "orders", 0, false, 0},
		{"其他服务不占用", 0, "other", 0, false, 0},
		{"其他数据中心不占用", 0, "orders", 1, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewLocalProvider(time.Second, time.Second, time.Hour)
			t.Cleanup(func() { require.NoError(t, p.Close()) })
			// 直接构造既有离线快照，避免用 sleep 模拟冷却到期；不启动后台协程。
			p.nodes["old"] = &NodeInfo{ID: "old", ServiceName: tc.service, DataCenterID: tc.dc,
				MachineID: 0, Status: NodeStatusOffline, LastHeartbeat: time.Now().Add(-tc.age)}
			ctx := context.Background()
			if tc.fillNext {
				require.NoError(t, p.Register(ctx, &NodeInfo{ID: "running", ServiceName: "orders", MachineID: 1}))
			}
			id := p.AllocateMachineID("orders", 0, 1)
			require.Equal(t, tc.want, id)
			if id >= 0 {
				require.NoError(t, p.Register(ctx, &NodeInfo{ID: "new", ServiceName: "orders", MachineID: id}))
			}
		})
	}
}
