// 本文件验证应用观测回调异常按未知处理，不在租约监视协程中击穿整个服务。
package ownership

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

type statusStore struct {
	Store
	credential Credential
}

func (s statusStore) Resolve(context.Context, string, string) (Credential, error) {
	return s.credential, nil
}
func (s statusStore) Validate(context.Context, Credential, bool) error { return nil }

func TestRuntimeObservationPanicIsUnknown(t *testing.T) {
	c := validCredential()
	var since atomic.Int64
	r := &Runtime{ctx: context.Background(), store: statusStore{credential: c}, options: Options{Manifest: Manifest{Scope: c.Scope}, InstanceID: c.InstanceID, SessionID: c.SessionID, Observe: func(context.Context, Credential) (Observation, error) { panic("应用观测异常") }}, status: map[string]*runtimeKeyProgress{c.Key: {context: context.Background(), credential: c, stageSince: &since}}}
	var err error
	panicked := false
	func() {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		_, err = r.Status(context.Background(), c.Key)
	}()
	if panicked {
		t.Fatal("观测异常不应击穿状态查询或租约监视")
	}
	if !errors.Is(err, ErrUnknown) {
		t.Fatalf("应保留观测未知，得到 %v", err)
	}
}
