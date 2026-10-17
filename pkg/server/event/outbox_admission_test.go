// 本文件验证服务准入失效时 Outbox 不加载或发布新消息，恢复后可继续。
package event

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"testing"
)

// 公共类型必须继续能作为 map key；函数型字段会使本编译契约失败。
var _ map[OutboxOptions]struct{}

type admissionProbeStore struct{ loads int }

func (s *admissionProbeStore) LoadPending(context.Context, int) ([]OutboxMessage, error) {
	s.loads++
	return nil, nil
}
func (s *admissionProbeStore) MarkPublished(context.Context, OutboxMessage) error { return nil }
func TestOutboxAdmissionStopsBeforeLoadAndPublish(t *testing.T) {
	unavailable := errors.New("not ready")
	store := &admissionProbeStore{}
	p := &outboxPublisher{store: store, admission: func() error { return unavailable }}
	p.drain(context.Background())
	require.Zero(t, store.loads)
	require.ErrorIs(t, p.publish(context.Background(), OutboxMessage{}), unavailable)
	p.admission = nil
	p.drain(context.Background())
	require.Equal(t, 1, store.loads)
}
