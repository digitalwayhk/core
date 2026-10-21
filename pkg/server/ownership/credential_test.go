// 本文件验证凭据、业务事务fence和迟到恢复快照的通用边界。
package ownership

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFenceStateNeverSerializesSecretToken(t *testing.T) {
	c := validCredential()
	value, err := json.Marshal(FenceState{Scope: c.Scope, Key: c.Key, Epoch: c.Epoch, Token: c.Token})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(value), c.Token) {
		t.Fatal("技术fence状态也不能将秘密token序列化到公开响应")
	}
}

func validCredential() Credential {
	return Credential{Scope: "stats", Key: "market:1", Service: "quotes", InstanceID: "instance-a", SessionID: "boot-a", Epoch: 3, Token: "private-token"}
}

func TestCredentialRejectsUnknownEpochAndIdentity(t *testing.T) {
	c := validCredential()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Credential){func(c *Credential) { c.Epoch = 0 }, func(c *Credential) { c.Token = "" }, func(c *Credential) { c.SessionID = "" }, func(c *Credential) { c.Key = "" }} {
		bad := c
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatal("未知凭据不能有效")
		}
	}
}

type fenceTransaction struct{ state FenceState }

func (f *fenceTransaction) LockFence(string, string) (FenceState, error) { return f.state, nil }
func (f *fenceTransaction) SaveFence(s FenceState) error                 { f.state = s; return nil }

func TestBusinessFenceRejectsOldOrRolledBackEpoch(t *testing.T) {
	tx := &fenceTransaction{}
	c := validCredential()
	if err := ActivateFence(tx, c); err != nil {
		t.Fatal(err)
	}
	if err := CheckFence(tx, c); err != nil {
		t.Fatal(err)
	}
	if err := ActivateFence(tx, c); err != nil {
		t.Fatal("同一凭据激活结果未知可幂等重试", err)
	}
	newer := c
	newer.Epoch++
	newer.Token = "next-token"
	if err := ActivateFence(tx, newer); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(CheckFence(tx, c), ErrFenced) {
		t.Fatal("旧持有者不得写")
	}
	if !errors.Is(ActivateFence(tx, c), ErrFenced) {
		t.Fatal("Redis回退epoch必须由SQL权威拒绝")
	}
	other := newer
	other.Token = "another-token"
	if !errors.Is(ActivateFence(tx, other), ErrFenced) {
		t.Fatal("同epoch不同token不能覆盖")
	}
}

func TestIdleUnknownAndDueWorkAreDifferent(t *testing.T) {
	now := time.Now()
	limits := DefaultTiming()
	idle := Observation{At: now, Delivered: 0, Unread: 0, Queued: 0, Due: 0}
	if err := idle.Check(now, now.Add(-time.Hour), now.Add(-time.Hour), limits); err != nil {
		t.Fatal("真实空闲不撤销", err)
	}
	work := idle
	work.Unread = 1
	if !errors.Is(work.Check(now, now.Add(-time.Hour), now, limits), ErrStalled) {
		t.Fatal("pending=0但未读卡住应撤销")
	}
	work.Unread = 0
	work.Due = 1
	if !errors.Is(work.Check(now, now.Add(-time.Hour), now, limits), ErrStalled) {
		t.Fatal("到期任务卡住应撤销")
	}
	unknown := idle
	unknown.Unread = -1
	if !errors.Is(unknown.Check(now, now, now.Add(-time.Hour), limits), ErrUnknown) {
		t.Fatal("未知不能当空闲")
	}
}

func TestLateSnapshotCannotResurrectUnsubscribe(t *testing.T) {
	view := NewReferenceView()
	older := ReferenceSnapshot{Revision: 1, References: []Reference{{GatewayID: "gateway-a", SessionID: "session-a", Name: "kline"}}}
	newer := ReferenceSnapshot{Revision: 2}
	if err := view.Apply(newer); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(view.Apply(older), ErrStaleSnapshot) {
		t.Fatal("迟到快照不得复活退订")
	}
	if len(view.References()) != 0 {
		t.Fatal("退订被旧快照覆盖")
	}
}
