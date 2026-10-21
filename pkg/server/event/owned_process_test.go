// 本文件用独立操作系统进程、正式ServiceContext和真实Redis验证持续跨主题归属。
package event_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/digitalwayhk/core/pkg/server/ownership"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type ownedProcessService struct{}

func (*ownedProcessService) ServiceName() string      { return "owned-fixture" }
func (*ownedProcessService) Routers() []types.IRouter { return nil }

type ownedProcessInput struct {
	Key, Body   string
	Block, Fail bool
}
type ownedProcessRecord struct {
	PID                        int
	Instance, Key, Phase, Body string
	Epoch                      int64
	Batch                      int
	Subjects                   []string
	References                 int
}

func ownedProcessManifest(prefix string, fast bool) ownership.Manifest {
	m := ownership.Manifest{Scope: prefix, Service: "owned-fixture", Group: "owned-fixture", Selector: "synthetic-key-v1", Subjects: []string{"one", "two"}, Keys: []string{"A", "B"}, Window: 8, MaxBatch: 8}
	if fast {
		m.Timing = ownership.Timing{Lease: 900 * time.Millisecond, Renew: 150 * time.Millisecond, Poll: 10 * time.Millisecond, Execution: 600 * time.Millisecond, Stall: 900 * time.Millisecond, Observation: 300 * time.Millisecond}
	}
	return m.Normalize()
}

// 子进程只通过正式订阅消费；控制Redis和审计列表均为隔离测试夹具，不是应用接入方式。
func TestOwnedConsumerProcessFixture(t *testing.T) {
	prefix := os.Getenv("CORE_OWNED_PROCESS_PREFIX")
	if prefix == "" {
		t.Skip("仅由持续归属测试子进程运行")
	}
	ctx := context.Background()
	control := redis.NewClient(&redis.Options{Addr: sharedControlRedisAddress()})
	defer control.Close()
	cfg := config.NewServiceDefaultConfig("owned-fixture", 29990)
	cfg.Cluster.Mode = "off"
	cfg.MQ.Mode = "on"
	cfg.MQ.Provider = "redis-stream"
	cfg.MQ.RedisStream = config.RedisStreamMQConfig{Addr: os.Getenv("CORE_TEST_REDIS_ADDR"), Prefix: prefix, CommandPoolSize: 4, BlockingReadPoolSize: 4}
	sc := router.NewServiceContextWithConfig(&ownedProcessService{}, cfg)
	defer func() { sc.SetRunState(false); require.NoError(t, sc.ShutdownError()) }()
	m := ownedProcessManifest(prefix, os.Getenv("CORE_OWNED_PROCESS_FAST") == "1")
	maxOwned, _ := strconv.Atoi(os.Getenv("CORE_OWNED_PROCESS_CAPACITY"))
	record := func(c ownership.Credential, phase string, rows []*event.Envelope, refs int) {
		subjects := []string{}
		for _, e := range rows {
			subjects = append(subjects, e.Subject)
		}
		if len(rows) == 0 {
			rows = []*event.Envelope{{}}
		}
		for _, e := range rows {
			var input ownedProcessInput
			_ = json.Unmarshal(e.Data, &input)
			b, _ := json.Marshal(ownedProcessRecord{PID: os.Getpid(), Instance: sc.ServiceInstanceID, Key: c.Key, Phase: phase, Body: input.Body, Epoch: c.Epoch, Batch: len(subjects), Subjects: subjects, References: refs})
			require.NoError(t, control.RPush(ctx, prefix+":records", b).Err())
		}
	}
	runtime, err := sc.SubscribeOwnedEvents(event.OwnedSubscription{Manifest: m, MaxOwned: maxOwned, NoDueTasks: true,
		Key: func(e *event.Envelope) (string, error) {
			var input ownedProcessInput
			err := json.Unmarshal(e.Data, &input)
			return input.Key, err
		},
		Activate: func(_ context.Context, c ownership.Credential) error { record(c, "activate", nil, 0); return nil },
		Restore: func(_ context.Context, c ownership.Credential, s ownership.ReferenceSnapshot) error {
			record(c, "restore", nil, len(s.References))
			return nil
		},
		Handler: func(work context.Context, c ownership.Credential, rows []*event.Envelope) error {
			record(c, "entered", rows, 0)
			for _, e := range rows {
				var input ownedProcessInput
				require.NoError(t, json.Unmarshal(e.Data, &input))
				if input.Block {
					for control.Get(ctx, prefix+":release:"+input.Body).Val() != "1" && control.Get(ctx, prefix+":release:"+input.Body+":"+strconv.Itoa(os.Getpid())).Val() != "1" {
						time.Sleep(5 * time.Millisecond)
					}
				}
				if input.Fail && control.Get(ctx, prefix+":allow:"+input.Body).Val() != "1" {
					record(c, "failed", rows, 0)
					return fmt.Errorf("受控未提交事务")
				}
			}
			record(c, "completed", rows, 0)
			return nil
		}})
	require.NoError(t, err)
	require.NoError(t, control.Set(ctx, prefix+":ready:"+strconv.Itoa(os.Getpid()), sc.ServiceInstanceID, 0).Err())
	for control.Get(ctx, prefix+":stop:"+strconv.Itoa(os.Getpid())).Val() != "1" {
		time.Sleep(10 * time.Millisecond)
	}
	require.NoError(t, runtime.Close(ctx))
}

type ownedProcessSuite struct {
	t         *testing.T
	ctx       context.Context
	client    *redis.Client
	prefix    string
	m         ownership.Manifest
	provider  *mq.RedisStreamProvider
	manager   *mq.MQManager
	store     ownership.Store
	publisher *event.MQBridge
}

func newOwnedProcessSuite(t *testing.T, fast bool) *ownedProcessSuite {
	t.Helper()
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT_RUN: CORE_TEST_REDIS_ADDR")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	prefix := fmt.Sprintf("core:owned-process:%d", time.Now().UnixNano())
	p := mq.NewRedisStreamProvider(addr, prefix, 0)
	require.NoError(t, p.Connect(ctx))
	t.Cleanup(func() { require.NoError(t, p.Close()) })
	manager := mq.NewManager()
	manager.Register(p)
	require.NoError(t, manager.SetCurrent(p.Name()))
	t.Cleanup(func() { require.NoError(t, manager.Close()) })
	manifest := ownedProcessManifest(prefix, fast)
	require.NoError(t, manager.ProvisionOwnership(ctx, manifest, 0))
	require.NoError(t, manager.BindOwnership(ctx, manifest))
	store, err := manager.OwnershipStore(ctx)
	require.NoError(t, err)
	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { client.Close() })
	return &ownedProcessSuite{t, ctx, client, prefix, manifest, p, manager, store, event.NewMQBridge(event.NewStream(), manager)}
}
func (s *ownedProcessSuite) start(capacity int, addr string) *exec.Cmd {
	s.t.Helper()
	if addr == "" {
		addr = os.Getenv("CORE_TEST_REDIS_ADDR")
	}
	fast := "0"
	if s.m.Timing.Lease < time.Second {
		fast = "1"
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestOwnedConsumerProcessFixture$", "-test.v")
	cmd.Env = append(os.Environ(), "CORE_OWNED_PROCESS_PREFIX="+s.prefix, "CORE_OWNED_PROCESS_CAPACITY="+strconv.Itoa(capacity), "CORE_OWNED_PROCESS_FAST="+fast, "CORE_TEST_REDIS_ADDR="+addr, "CORE_SHARED_CONTROL_REDIS_ADDR="+os.Getenv("CORE_TEST_REDIS_ADDR"))
	f, err := os.CreateTemp(s.t.TempDir(), "owner-*.log")
	require.NoError(s.t, err)
	cmd.Stdout = f
	cmd.Stderr = f
	require.NoError(s.t, cmd.Start())
	var waited atomic.Bool
	s.t.Cleanup(func() {
		if !waited.Swap(true) {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		f.Close()
		if s.t.Failed() {
			b, _ := os.ReadFile(f.Name())
			s.t.Log(string(b))
		}
	})
	require.Eventually(s.t, func() bool {
		return s.client.Exists(s.ctx, s.prefix+":ready:"+strconv.Itoa(cmd.Process.Pid)).Val() == 1
	}, 5*time.Second, 10*time.Millisecond)
	return cmd
}
func (s *ownedProcessSuite) records() []ownedProcessRecord {
	raw, err := s.client.LRange(s.ctx, s.prefix+":records", 0, -1).Result()
	require.NoError(s.t, err)
	rows := make([]ownedProcessRecord, 0, len(raw))
	for _, b := range raw {
		var row ownedProcessRecord
		require.NoError(s.t, json.Unmarshal([]byte(b), &row))
		rows = append(rows, row)
	}
	return rows
}
func (s *ownedProcessSuite) wait(body, phase string, pid int, timeout time.Duration) ownedProcessRecord {
	var found ownedProcessRecord
	require.Eventually(s.t, func() bool {
		for _, r := range s.records() {
			if r.Body == body && r.Phase == phase && (pid == 0 || r.PID == pid) {
				found = r
				return true
			}
		}
		return false
	}, timeout, 10*time.Millisecond, "缺少 %s/%s PID=%d", body, phase, pid)
	return found
}
func (s *ownedProcessSuite) owner(key string, pid int) ownership.Credential {
	var c ownership.Credential
	instance := s.client.Get(s.ctx, s.prefix+":ready:"+strconv.Itoa(pid)).Val()
	require.Eventually(s.t, func() bool {
		var err error
		c, err = s.store.Resolve(s.ctx, s.m.Scope, key)
		return err == nil && c.InstanceID == instance
	}, 4*time.Second, 10*time.Millisecond)
	return c
}
func (s *ownedProcessSuite) publish(subject, key, body string, block, fail bool) {
	data, _ := json.Marshal(ownedProcessInput{key, body, block, fail})
	e := event.NewEnvelope("fixture", "changed", data)
	e.Subject = subject
	e.ShardKey = "original:" + key
	require.NoError(s.t, s.publisher.Publish(s.ctx, subject, e))
}
func (s *ownedProcessSuite) pending() int64 {
	var n int64
	for _, subject := range s.m.Subjects {
		n += s.client.XPending(s.ctx, s.prefix+":"+subject, s.m.Group).Val().Count
	}
	return n
}

// 验证原生租约的真实多进程批次、错误屏障、硬退出PEL及成员正常退出恢复。
func TestOwnedTwoProcessesMultiSubjectBatchAndNativeCrash(t *testing.T) {
	s := newOwnedProcessSuite(t, false)
	// 在订阅开始前发布，使真实批次可稳定包含两个subject。
	for _, subject := range s.m.Subjects {
		for i := 0; i < 4; i++ {
			s.publish(subject, "A", fmt.Sprintf("%s-A%d", subject, i), false, false)
		}
	}
	a := s.start(1, "")
	old := s.owner("A", a.Process.Pid)
	b := s.start(2, "")
	s.owner("B", b.Process.Pid)
	require.NotEqual(t, a.Process.Pid, b.Process.Pid)
	s.wait("two-A3", "completed", a.Process.Pid, 5*time.Second)
	multi := false
	for _, r := range s.records() {
		if r.Phase == "completed" {
			require.LessOrEqual(t, r.Batch, 8)
			if r.Batch > 1 && len(r.Subjects) > 1 {
				multi = true
			}
		}
	}
	require.True(t, multi, "必须有多输入业务批次")
	require.Eventually(t, func() bool { return s.pending() == 0 }, 2*time.Second, 10*time.Millisecond)
	s.publish("one", "A", "blocked-A", true, true)
	s.wait("blocked-A", "entered", a.Process.Pid, 3*time.Second)
	s.publish("one", "A", "later-A", false, false)
	s.publish("two", "B", "independent-B", false, false)
	s.wait("independent-B", "completed", b.Process.Pid, 3*time.Second)
	for _, r := range s.records() {
		require.NotEqual(t, "later-A", r.Body)
	}
	require.NoError(t, s.client.Set(s.ctx, s.prefix+":release:blocked-A", "1", 0).Err())
	s.wait("blocked-A", "failed", a.Process.Pid, 3*time.Second)
	for _, r := range s.records() {
		require.NotEqual(t, "later-A", r.Body, "失败前序未提交不能越过")
	}
	killed := time.Now()
	require.NoError(t, a.Process.Kill())
	require.Error(t, a.Wait())
	require.NoError(t, s.client.Set(s.ctx, s.prefix+":allow:blocked-A", "1", 0).Err())
	s.wait("later-A", "completed", b.Process.Pid, 20*time.Second)
	newOwner := s.owner("A", b.Process.Pid)
	require.Greater(t, newOwner.Epoch, old.Epoch)
	require.ErrorIs(t, s.store.Release(s.ctx, old), ownership.ErrFenced)
	require.Eventually(t, func() bool { return s.pending() == 0 }, 3*time.Second, 10*time.Millisecond)
	counts := map[string]int{}
	for _, r := range s.records() {
		if r.Phase == "completed" {
			counts[r.Body]++
		}
	}
	for body, n := range counts {
		require.Equal(t, 1, n, body+"已ACK工作不能异常重放")
	}
	require.NoError(t, s.client.Set(s.ctx, s.prefix+":stop:"+strconv.Itoa(b.Process.Pid), "1", 0).Err())
	require.NoError(t, b.Wait())
	c := s.start(2, "")
	s.owner("A", c.Process.Pid)
	s.publish("one", "A", "after-graceful", false, false)
	s.wait("after-graceful", "completed", c.Process.Pid, 3*time.Second)
	t.Logf("真实PID=%d/%d/%d；原生15s租约硬崩溃恢复=%s；原组/Stream保留", a.Process.Pid, b.Process.Pid, c.Process.Pid, time.Since(killed))
}

// 加速租约仅用于受控断连：旧handler仍运行时，迟到完成不得ACK或删除新租约。
func TestOwnedProcessNetworkLossLateCompletionAndReferenceRestore(t *testing.T) {
	s := newOwnedProcessSuite(t, true)
	proxy := newSharedRedisProxy(t, os.Getenv("CORE_TEST_REDIS_ADDR"))
	a := s.start(1, proxy.listener.Addr().String())
	old := s.owner("A", a.Process.Pid)
	b := s.start(2, "")
	s.owner("B", b.Process.Pid)
	require.NoError(t, s.store.OpenGateway(s.ctx, "gw", time.Minute))
	require.NoError(t, s.store.ReplaceGateway(s.ctx, ownership.GatewaySnapshot{GatewayID: "gw", Revision: 1, Lease: time.Minute, References: []ownership.ScopedReference{{Scope: s.m.Scope, Key: "A", SessionID: "existing", Name: "view"}}}))
	s.publish("one", "A", "slow-A", true, false)
	s.wait("slow-A", "entered", a.Process.Pid, 3*time.Second)
	proxy.disconnect(true)
	s.wait("slow-A", "entered", b.Process.Pid, 4*time.Second)
	current, err := s.store.Resolve(s.ctx, s.m.Scope, "A")
	require.NoError(t, err)
	require.Greater(t, current.Epoch, old.Epoch)
	proxy.disconnect(false)
	require.NoError(t, s.client.Set(s.ctx, s.prefix+":release:slow-A:"+strconv.Itoa(a.Process.Pid), "1", 0).Err())
	s.wait("slow-A", "completed", a.Process.Pid, 3*time.Second)
	require.ErrorIs(t, s.store.Release(s.ctx, old), ownership.ErrFenced)
	require.NoError(t, s.store.Validate(s.ctx, current, true))
	require.EqualValues(t, 1, s.pending(), "迟到旧handler不能ACK")
	restored := false
	for _, r := range s.records() {
		if r.PID == b.Process.Pid && r.Key == "A" && r.Phase == "restore" && r.References == 1 {
			restored = true
		}
	}
	require.True(t, restored, "新owner自动恢复已有Gateway引用，无需客户端重订")
	require.NoError(t, s.client.Set(s.ctx, s.prefix+":release:slow-A:"+strconv.Itoa(b.Process.Pid), "1", 0).Err())
	s.wait("slow-A", "completed", b.Process.Pid, 3*time.Second)
	require.Eventually(t, func() bool { return s.pending() == 0 }, 2*time.Second, 10*time.Millisecond)
	t.Logf("加速lease=%s；失联PID=%d，新ownerPID=%d", s.m.Timing.Lease, a.Process.Pid, b.Process.Pid)
}
