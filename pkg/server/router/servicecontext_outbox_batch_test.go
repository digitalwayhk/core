// 本文件验证服务组合根向标准Outbox传递批量上限，零值仍保持历史行为。
package router

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/stretchr/testify/require"
)

type batchLimitOutboxStore struct{ limits chan int }

func (s batchLimitOutboxStore) LoadPending(_ context.Context, limit int) ([]event.OutboxMessage, error) {
	select {
	case s.limits <- limit:
	default:
	}
	return nil, nil
}

func (batchLimitOutboxStore) MarkPublished(context.Context, event.OutboxMessage) error { return nil }

// TestServiceContextOutboxBatchLimit 验证实际扫描上限，而非只比较选项字段。
func TestServiceContextOutboxBatchLimit(t *testing.T) {
	for _, test := range []struct {
		name    string
		options string
		want    int
	}{
		{"默认100", `{}`, 100},
		{"显式512", `{"BatchSize":512}`, 512},
		{"负值沿用底层默认", `{"BatchSize":-1}`, 100},
	} {
		t.Run(test.name, func(t *testing.T) {
			name := fmt.Sprintf("outbox-batch-%d", time.Now().UnixNano())
			cfg := config.NewServiceDefaultConfig(name, 0)
			cfg.Cluster.Mode, cfg.MQ.Mode = "off", "off"
			sc := NewServiceContextWithConfig(&metricsTestService{name: name}, cfg)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				require.NoError(t, sc.ServiceEventBridge.Close(ctx))
			})
			var options OutboxRuntimeOptions
			require.NoError(t, json.Unmarshal([]byte(test.options), &options))
			limits := make(chan int, 1)
			require.NoError(t, sc.UseOutboxWithOptions(batchLimitOutboxStore{limits: limits}, options))
			sc.NotifyOutbox()
			select {
			case got := <-limits:
				require.Equal(t, test.want, got)
			case <-time.After(time.Second):
				t.Fatal("标准Outbox未执行扫描")
			}
		})
	}
}
