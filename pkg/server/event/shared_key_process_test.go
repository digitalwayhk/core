package event_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type sharedProcessEvent struct {
	Body  string
	Block bool
	Fail  bool
}
type sharedProcessRecord struct {
	PID   int
	Body  string
	Key   string
	Phase string
	At    int64
}

func TestSharedKeyConsumerProcessFixture(t *testing.T) {
	prefix := os.Getenv("CORE_SHARED_PROCESS_PREFIX")
	if prefix == "" {
		t.Skip("仅由真实消费者子进程执行")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := redis.NewClient(&redis.Options{Addr: sharedControlRedisAddress()})
	defer client.Close()
	provider := mq.NewRedisStreamProvider(os.Getenv("CORE_TEST_REDIS_ADDR"), prefix, 0)
	require.NoError(t, provider.Connect(ctx))
	manager := mq.NewManager()
	manager.Register(provider)
	require.NoError(t, manager.SetCurrent(provider.Name()))
	stream := event.NewStream()
	bridge := event.NewServiceEventBridge(stream, event.ServiceEventBridgeOptions{SubscriberID: "users"})
	bridge.SetExternalPublisher(event.NewMQBridge(stream, manager))
	sub := event.Subscription{}
	// JSON 让旧基线也能运行同一夹具；旧版本忽略新增要求后将得到真实行为 RED。
	require.NoError(t, json.Unmarshal([]byte(`{"Subject":"fills","Reliable":true,"SharedKeys":true,"KeyConcurrency":1}`), &sub))
	sub.Handler = func(_ context.Context, env *event.Envelope) error {
		var data sharedProcessEvent
		if err := json.Unmarshal(env.Data, &data); err != nil {
			return err
		}
		record := func(phase string) {
			b, _ := json.Marshal(sharedProcessRecord{os.Getpid(), data.Body, env.ShardKey, phase, time.Now().UnixNano()})
			require.NoError(t, client.RPush(ctx, prefix+":records", b).Err())
		}
		record("entered")
		if data.Block {
			for client.Get(ctx, prefix+":release:"+data.Body).Val() != "1" && client.Get(ctx, prefix+":release:"+data.Body+":"+strconv.Itoa(os.Getpid())).Val() != "1" {
				time.Sleep(10 * time.Millisecond)
			}
		}
		if data.Fail && client.Get(ctx, prefix+":allow:"+data.Body).Val() != "1" {
			record("failed")
			return errors.New("fixture failure barrier")
		}
		record("completed")
		return nil
	}
	stop, err := bridge.SubscribeEvent(sub)
	require.NoError(t, err)
	require.NoError(t, client.Set(ctx, prefix+":ready:"+strconv.Itoa(os.Getpid()), "1", 0).Err())
	for client.Get(ctx, prefix+":stop:"+strconv.Itoa(os.Getpid())).Val() != "1" {
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	require.NoError(t, bridge.Close(context.Background()))
	require.NoError(t, manager.Close())
}

type sharedProcessSuite struct {
	t         *testing.T
	ctx       context.Context
	client    *redis.Client
	prefix    string
	publisher *event.MQBridge
}

func newSharedProcessSuite(t *testing.T) *sharedProcessSuite {
	t.Helper()
	addr := os.Getenv("CORE_TEST_REDIS_ADDR")
	if addr == "" {
		t.Skip("NOT_RUN: CORE_TEST_REDIS_ADDR")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	t.Cleanup(cancel)
	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = client.Close() })
	prefix := fmt.Sprintf("core:shared-process:%d", time.Now().UnixNano())
	p := mq.NewRedisStreamProvider(addr, prefix, 0)
	require.NoError(t, p.Connect(ctx))
	t.Cleanup(func() { require.NoError(t, p.Close()) })
	m := mq.NewManager()
	m.Register(p)
	require.NoError(t, m.SetCurrent(p.Name()))
	return &sharedProcessSuite{t, ctx, client, prefix, event.NewMQBridge(event.NewStream(), m)}
}
func sharedControlRedisAddress() string {
	if addr := os.Getenv("CORE_SHARED_CONTROL_REDIS_ADDR"); addr != "" {
		return addr
	}
	return os.Getenv("CORE_TEST_REDIS_ADDR")
}
func (s *sharedProcessSuite) start() *exec.Cmd {
	return s.startWithBroker(os.Getenv("CORE_TEST_REDIS_ADDR"))
}
func (s *sharedProcessSuite) startWithBroker(addr string) *exec.Cmd {
	s.t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestSharedKeyConsumerProcessFixture$", "-test.v")
	cmd.Env = append(os.Environ(), "CORE_SHARED_PROCESS_PREFIX="+s.prefix, "CORE_TEST_REDIS_ADDR="+addr, "CORE_SHARED_CONTROL_REDIS_ADDR="+os.Getenv("CORE_TEST_REDIS_ADDR"))
	file, err := os.CreateTemp(s.t.TempDir(), "consumer-*.log")
	require.NoError(s.t, err)
	cmd.Stdout = file
	cmd.Stderr = file
	require.NoError(s.t, cmd.Start())
	s.t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		_ = file.Close()
		if s.t.Failed() {
			b, _ := os.ReadFile(file.Name())
			s.t.Log(string(b))
		}
	})
	require.Eventually(s.t, func() bool {
		return s.client.Exists(s.ctx, s.prefix+":ready:"+strconv.Itoa(cmd.Process.Pid)).Val() == 1
	}, 5*time.Second, 10*time.Millisecond, "消费者进程未就绪")
	return cmd
}
func (s *sharedProcessSuite) publish(key, body string, block, fail bool) {
	s.t.Helper()
	data, err := json.Marshal(sharedProcessEvent{body, block, fail})
	require.NoError(s.t, err)
	env := event.NewEnvelope("fixture", "changed", data)
	env.Subject = "fills"
	env.ShardKey = key
	require.NoError(s.t, s.publisher.Publish(s.ctx, "fills", env))
}
func (s *sharedProcessSuite) records() []sharedProcessRecord {
	raw, err := s.client.LRange(s.ctx, s.prefix+":records", 0, -1).Result()
	require.NoError(s.t, err)
	records := make([]sharedProcessRecord, 0, len(raw))
	for _, b := range raw {
		var r sharedProcessRecord
		require.NoError(s.t, json.Unmarshal([]byte(b), &r))
		records = append(records, r)
	}
	return records
}
func (s *sharedProcessSuite) wait(body, phase string) sharedProcessRecord {
	s.t.Helper()
	var found sharedProcessRecord
	require.Eventually(s.t, func() bool {
		for _, r := range s.records() {
			if r.Body == body && r.Phase == phase {
				found = r
				return true
			}
		}
		return false
	}, 3*time.Second, 10*time.Millisecond, "缺少 %s/%s", body, phase)
	return found
}
func (s *sharedProcessSuite) release(body string) {
	s.t.Helper()
	require.NoError(s.t, s.client.Set(s.ctx, s.prefix+":release:"+body, "1", 0).Err())
}

func TestSharedKeyReliableTwoProcessesShareSameLogicalGroup(t *testing.T) {
	s := newSharedProcessSuite(t)
	a := s.start()
	s.publish("user:A", "A1", true, false)
	first := s.wait("A1", "entered")
	require.Equal(t, a.Process.Pid, first.PID)
	b := s.start()
	s.publish("user:B", "B1", false, false)
	second := s.wait("B1", "completed")
	require.Equal(t, b.Process.Pid, second.PID, "慢 key A 不得让另一消费者进程空闲")
	s.publish("user:B", "B2", false, false)
	s.wait("B2", "completed")
	for _, r := range s.records() {
		require.False(t, r.Body == "A1" && r.Phase == "completed")
	}
	s.release("A1")
	s.wait("A1", "completed")
	t.Logf("真实 PID A=%d B=%d，同 subject=fills group=users", a.Process.Pid, b.Process.Pid)
}

func TestSharedKeyReliableFailureAndMemberExit(t *testing.T) {
	s := newSharedProcessSuite(t)
	a := s.start()
	s.publish("user:A", "A1", true, true)
	s.wait("A1", "entered")
	b := s.start()
	s.publish("user:A", "A2", false, false)
	s.publish("user:B", "B1", false, false)
	s.wait("B1", "completed")
	s.release("A1")
	s.wait("A1", "failed")
	s.publish("user:B", "B2", false, false)
	s.wait("B2", "completed")
	for _, r := range s.records() {
		require.NotEqual(t, "A2", r.Body, "失败前序未完成时不能执行 A2")
	}
	require.NoError(t, s.client.Set(s.ctx, s.prefix+":allow:A1", "1", 0).Err())
	s.wait("A1", "completed")
	s.wait("A2", "completed")
	require.NoError(t, s.client.Set(s.ctx, s.prefix+":stop:"+strconv.Itoa(a.Process.Pid), "1", 0).Err())
	exited := make(chan error, 1)
	go func() { exited <- a.Wait() }()
	select {
	case err := <-exited:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("正常退出未完成")
	}
	s.publish("user:A", "A3", false, false)
	third := s.wait("A3", "completed")
	require.Equal(t, b.Process.Pid, third.PID)
	c := s.start()
	s.publish("user:C", "C1", true, false)
	s.wait("C1", "entered")
	s.publish("user:D", "D1", false, false)
	s.wait("D1", "completed")
	s.release("C1")
	s.wait("C1", "completed")
	require.NotEqual(t, a.Process.Pid, c.Process.Pid, "新成员使用独立会话")
	last := map[string]string{}
	for _, r := range s.records() {
		if r.Phase == "entered" {
			require.Empty(t, last[r.Key], "同 key 真实 handler 发生重叠")
			last[r.Key] = r.Body
		} else if r.Phase == "completed" || r.Phase == "failed" {
			require.Equal(t, r.Body, last[r.Key])
			delete(last, r.Key)
		}
	}
}

func TestSharedKeyReliableOldPendingAndNewMessages(t *testing.T) {
	s := newSharedProcessSuite(t)
	for i := 0; i < 24; i++ {
		for _, key := range []string{"A", "B", "C"} {
			s.publish(key, fmt.Sprintf("%s%02d", key, i), false, false)
		}
	}
	stream := s.prefix + ":fills"
	require.NoError(t, s.client.XGroupCreate(s.ctx, stream, "users", "0").Err())
	entries, err := s.client.XReadGroup(s.ctx, &redis.XReadGroupArgs{Group: "users", Consumer: "legacy-crashed", Streams: []string{stream, ">"}, Count: 72}).Result()
	require.NoError(t, err)
	require.Len(t, entries[0].Messages, 72)
	s.start()
	s.start()
	s.publish("A", "A24", false, false)
	s.publish("B", "B24", false, false)
	require.Eventually(t, func() bool {
		n := 0
		for _, r := range s.records() {
			if r.Phase == "completed" {
				n++
			}
		}
		return n == 74
	}, 10*time.Second, 10*time.Millisecond)
	next := map[string]int{}
	for _, r := range s.records() {
		if r.Phase != "completed" {
			continue
		}
		var index int
		_, err := fmt.Sscanf(r.Body, r.Key+"%02d", &index)
		require.NoError(t, err)
		require.Equal(t, next[r.Key], index, "旧 PEL 与恢复期间的新消息顺序")
		next[r.Key]++
	}
	require.Eventually(t, func() bool { return s.client.XPending(s.ctx, stream, "users").Val().Count == 0 }, time.Second, 10*time.Millisecond)
	require.EqualValues(t, 74, s.client.XLen(s.ctx, stream).Val(), "不能靠删除原消息或重建组恢复")
}

func TestSharedKeyReliableHardCrashAndFullLeaseRecovery(t *testing.T) {
	if os.Getenv("CORE_TEST_SHARED_FULL_LEASE") != "1" {
		t.Skip("NOT_RUN: CORE_TEST_SHARED_FULL_LEASE=1，实际等待原生租约")
	}
	s := newSharedProcessSuite(t)
	a := s.start()
	s.publish("user:A", "A1", true, false)
	s.wait("A1", "entered")
	b := s.start()
	killed := time.Now()
	require.NoError(t, a.Process.Kill())
	require.Error(t, a.Wait())
	s.release("A1")
	s.publish("user:A", "A2", false, false)
	s.publish("user:B", "B1", false, false)
	s.wait("B1", "completed")
	require.Eventually(t, func() bool {
		for _, r := range s.records() {
			if r.Body == "A2" && r.Phase == "completed" {
				return true
			}
		}
		return false
	}, 150*time.Second, 20*time.Millisecond)
	var completed []sharedProcessRecord
	for _, r := range s.records() {
		if r.Key == "user:A" && r.Phase == "completed" {
			completed = append(completed, r)
		}
	}
	require.Len(t, completed, 2)
	require.Equal(t, "A1", completed[0].Body)
	require.Equal(t, "A2", completed[1].Body)
	require.Equal(t, b.Process.Pid, completed[0].PID)
	t.Logf("硬杀 PID=%d，原生 lease 未加速；接管 PID=%d，恢复耗时=%s", a.Process.Pid, b.Process.Pid, time.Since(killed))
	c := s.start()
	s.publish("user:C", "C1", true, false)
	s.wait("C1", "entered")
	s.publish("user:D", "D1", false, false)
	s.wait("D1", "completed")
	s.release("C1")
	s.wait("C1", "completed")
	require.NotEqual(t, b.Process.Pid, c.Process.Pid)
}
