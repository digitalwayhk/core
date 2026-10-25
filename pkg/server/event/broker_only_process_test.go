// 本文件通过真实Redis、独立ServiceContext进程、原生租约及SQLite Inbox事务验证恢复。
package event_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/digitalwayhk/core/pkg/persistence/database/oltp"
	"github.com/digitalwayhk/core/pkg/persistence/entity"
	"github.com/digitalwayhk/core/pkg/persistence/message"
	pt "github.com/digitalwayhk/core/pkg/persistence/types"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type brokerInboxFact struct {
	*entity.Model
	EventID string `gorm:"uniqueIndex"`
}

func brokerFact(id string) *brokerInboxFact {
	f := &brokerInboxFact{Model: entity.NewModel(), EventID: id}
	f.SetHashcode(id)
	return f
}
func brokerInbox(t *testing.T, name string) *oltp.Sqlite {
	t.Helper()
	a := oltp.NewFixedSqlite(name)
	var rows []*brokerInboxFact
	require.NoError(t, a.Load(&pt.SearchItem{Model: brokerFact(""), Page: 1, Size: 1, SkipCount: true}, &rows))
	return a
}
func brokerProcessPolicy() mq.LifecyclePolicy {
	return mq.LifecyclePolicy{Subject: "events", Mode: mq.LifecycleModeEnforce, RequiredGroups: []mq.ConsumerGroupRequirement{{Name: "broker-only-fixture", Start: mq.StartFromAllRetained}}, Retention: mq.RetentionPolicy{MinAge: time.Hour}, Retry: mq.RetryPolicy{MaxDeliveries: 4, Backoff: []time.Duration{200 * time.Millisecond}, DeadLetterSubject: "failed-events", HandlerTimeout: time.Minute, MaxAckPending: 8}, Capacity: mq.CapacityPolicy{HardMessages: 1000}}
}

// TestBrokerOnlyConsumerProcessFixture 是真实OS子进程入口；父测试核对PID、PEL和事务事实。
func TestBrokerOnlyConsumerProcessFixture(t *testing.T) {
	prefix := os.Getenv("CORE_BROKER_ONLY_PROCESS_PREFIX")
	if prefix == "" {
		t.Skip("父测试启动的独立进程夹具")
	}
	ctx := context.Background()
	control := redis.NewClient(&redis.Options{Addr: os.Getenv("CORE_TEST_REDIS_ADDR")})
	defer control.Close()
	sc := brokerOnlyContext(t, prefix)
	require.NoError(t, sc.RequireMessageLifecycle(ctx, brokerProcessPolicy()))
	if os.Getenv("CORE_BROKER_ONLY_ROLE") == "publisher" {
		db, err := badger.Open(badger.DefaultOptions(os.Getenv("CORE_BROKER_ONLY_OUTBOX")).WithSyncWrites(true).WithLogger(nil))
		require.NoError(t, err)
		defer db.Close()
		store := &brokerProcessOutbox{db: db, control: control, prefix: prefix, block: os.Getenv("CORE_BROKER_ONLY_BLOCK_MARK") == "1"}
		require.NoError(t, store.seed())
		require.NoError(t, sc.UseOutbox(store))
		sc.NotifyOutbox()
	} else {
		a := brokerInbox(t, os.Getenv("CORE_BROKER_ONLY_DB"))
		record := func(e *event.Envelope, phase string) {
			b, _ := json.Marshal(sharedProcessRecord{PID: os.Getpid(), Body: string(e.Data), Key: e.ShardKey, Phase: phase})
			require.NoError(t, control.RPush(ctx, prefix+":records", b).Err())
		}
		stop, err := sc.SubscribeEvent(event.Subscription{Subject: "events", Reliable: true, BrokerOnly: true, SharedKeys: true, KeyConcurrency: 1, Prefetch: 8, Handler: func(work context.Context, e *event.Envelope) error {
			record(e, "entered")
			if string(e.Data) == "A1" {
				for control.Get(ctx, prefix+":release:A1").Val() != "1" {
					select {
					case <-work.Done():
						return work.Err()
					case <-time.After(10 * time.Millisecond):
					}
				}
			}
			if string(e.Data) == "C1" && control.Get(ctx, prefix+":allow:C1").Val() != "1" {
				record(e, "failed")
				return errors.New("受控事务未提交")
			}
			receipt := brokerFact("inbox:" + e.ID)
			q := &pt.SearchItem{}
			q.AddWhereN("EventID", receipt.EventID)
			err := message.Run(work, a, func(tx pt.IDataAction) error {
				return message.ConsumeInboxWith(work, tx, receipt, q, func(tx pt.IDataAction) error { return tx.Insert(brokerFact("effect:" + e.ID)) })
			})
			if err == nil {
				record(e, "completed")
			}
			return err
		}})
		require.NoError(t, err)
		defer stop()
	}
	require.NoError(t, control.Set(ctx, prefix+":ready:"+strconv.Itoa(os.Getpid()), "1", 0).Err())
	for control.Get(ctx, prefix+":stop:"+strconv.Itoa(os.Getpid())).Val() != "1" {
		time.Sleep(10 * time.Millisecond)
	}
}

type brokerPersistedMessage struct {
	Message   event.OutboxMessage
	Published bool
}
type brokerProcessOutbox struct {
	db      *badger.DB
	control *redis.Client
	prefix  string
	block   bool
}

func (s *brokerProcessOutbox) seed() error {
	return s.db.Update(func(tx *badger.Txn) error {
		_, err := tx.Get([]byte("outbox"))
		if err == nil {
			return nil
		}
		if !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}
		v := brokerPersistedMessage{Message: event.OutboxMessage{ID: 1, EventID: "stable-outbox-event", EventType: "changed", Subject: "events", ShardKey: "A", Payload: []byte("duplicate")}}
		b, _ := json.Marshal(v)
		return tx.Set([]byte("outbox"), b)
	})
}
func (s *brokerProcessOutbox) LoadPending(context.Context, int) ([]event.OutboxMessage, error) {
	var v brokerPersistedMessage
	err := s.db.View(func(tx *badger.Txn) error {
		i, e := tx.Get([]byte("outbox"))
		if e != nil {
			return e
		}
		return i.Value(func(b []byte) error { return json.Unmarshal(b, &v) })
	})
	if err != nil {
		return nil, err
	}
	if v.Published {
		return nil, nil
	}
	return []event.OutboxMessage{v.Message}, nil
}
func (s *brokerProcessOutbox) MarkPublished(ctx context.Context, _ event.OutboxMessage) error {
	if s.block {
		if err := s.control.Set(ctx, s.prefix+":mark-entered", "1", 0).Err(); err != nil {
			return err
		}
		for s.control.Get(ctx, s.prefix+":allow-mark").Val() != "1" {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	err := s.db.Update(func(tx *badger.Txn) error {
		i, e := tx.Get([]byte("outbox"))
		if e != nil {
			return e
		}
		var v brokerPersistedMessage
		if e = i.Value(func(b []byte) error { return json.Unmarshal(b, &v) }); e != nil {
			return e
		}
		v.Published = true
		b, e := json.Marshal(v)
		if e != nil {
			return e
		}
		return tx.Set([]byte("outbox"), b)
	})
	if err == nil {
		return s.control.Set(ctx, s.prefix+":marked", "1", 0).Err()
	}
	return err
}

type brokerProcessSuite struct {
	t                      *testing.T
	sc                     *router.ServiceContext
	client                 *redis.Client
	prefix, dbname, outbox string
	db                     *oltp.Sqlite
}

func newBrokerProcessSuite(t *testing.T) *brokerProcessSuite {
	t.Helper()
	prefix := fmt.Sprintf("core:broker-only:process:%d", time.Now().UnixNano())
	sc := brokerOnlyContext(t, prefix)
	require.NoError(t, sc.RequireMessageLifecycle(context.Background(), brokerProcessPolicy()))
	client := redis.NewClient(&redis.Options{Addr: os.Getenv("CORE_TEST_REDIS_ADDR")})
	t.Cleanup(func() { client.Close() })
	name := fmt.Sprintf("broker_only_%d", time.Now().UnixNano())
	db := brokerInbox(t, name)
	t.Cleanup(func() { require.NoError(t, db.DeleteDB()) })
	return &brokerProcessSuite{t, sc, client, prefix, name, filepath.Join(t.TempDir(), "outbox"), db}
}
func (s *brokerProcessSuite) start(role string, blockMark bool) *exec.Cmd {
	s.t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestBrokerOnlyConsumerProcessFixture$", "-test.v")
	cmd.Env = append(os.Environ(), "CORE_BROKER_ONLY_PROCESS_PREFIX="+s.prefix, "CORE_BROKER_ONLY_DB="+s.dbname, "CORE_BROKER_ONLY_ROLE="+role, "CORE_BROKER_ONLY_OUTBOX="+s.outbox)
	if blockMark {
		cmd.Env = append(cmd.Env, "CORE_BROKER_ONLY_BLOCK_MARK=1")
	}
	log, err := os.CreateTemp(s.t.TempDir(), "broker-process-*.log")
	require.NoError(s.t, err)
	cmd.Stdout = log
	cmd.Stderr = log
	require.NoError(s.t, cmd.Start())
	s.t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		log.Close()
		if s.t.Failed() {
			raw, _ := os.ReadFile(log.Name())
			s.t.Log(string(raw))
		}
	})
	require.Eventually(s.t, func() bool {
		return s.client.Exists(context.Background(), s.prefix+":ready:"+strconv.Itoa(cmd.Process.Pid)).Val() == 1
	}, 8*time.Second, 10*time.Millisecond)
	return cmd
}
func (s *brokerProcessSuite) publish(key, body string) {
	s.t.Helper()
	e := brokerEnvelope("events", "changed")
	e.Data = []byte(body)
	e.ID = body
	e.ShardKey = key
	require.NoError(s.t, s.sc.ServiceEventBridge.Publish(context.Background(), event.PublishRequest{Class: event.ControlDelivery, External: true, Subject: e.Subject, Envelope: e}))
}
func (s *brokerProcessSuite) records() []sharedProcessRecord {
	raw, err := s.client.LRange(context.Background(), s.prefix+":records", 0, -1).Result()
	require.NoError(s.t, err)
	out := []sharedProcessRecord{}
	for _, b := range raw {
		var r sharedProcessRecord
		require.NoError(s.t, json.Unmarshal([]byte(b), &r))
		out = append(out, r)
	}
	return out
}
func (s *brokerProcessSuite) wait(body, phase string) sharedProcessRecord {
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
	}, 8*time.Second, 10*time.Millisecond, "%s/%s", body, phase)
	return found
}
func (s *brokerProcessSuite) flag(name string) {
	s.t.Helper()
	require.NoError(s.t, s.client.Set(context.Background(), s.prefix+":"+name, "1", 0).Err())
}
func (s *brokerProcessSuite) pending() int64 {
	pending, err := s.client.XPending(context.Background(), s.prefix+":events", "broker-only-fixture").Result()
	require.NoError(s.t, err)
	return pending.Count
}

// TestBrokerOnlyTwoProcessesOrderingAndFailure 验证相同逻辑组两PID并行，同key失败不越序，真实Inbox只在事务完成后提交。
func TestBrokerOnlyTwoProcessesOrderingAndFailure(t *testing.T) {
	s := newBrokerProcessSuite(t)
	a := s.start("consumer", false)
	s.publish("A", "A1")
	require.Equal(t, a.Process.Pid, s.wait("A1", "entered").PID)
	b := s.start("consumer", false)
	s.publish("A", "A2")
	s.publish("B", "B1")
	require.Equal(t, b.Process.Pid, s.wait("B1", "completed").PID)
	for _, r := range s.records() {
		require.NotEqual(t, "A2", r.Body)
	}
	require.Greater(t, s.pending(), int64(0))
	s.flag("release:A1")
	s.wait("A1", "completed")
	s.wait("A2", "completed")
	s.publish("C", "C1")
	s.wait("C1", "failed")
	s.publish("C", "C2")
	s.publish("D", "D1")
	s.wait("D1", "completed")
	for _, r := range s.records() {
		require.False(t, r.Body == "C2" && r.Phase == "entered")
	}
	var rows []*brokerInboxFact
	require.NoError(t, s.db.Load(&pt.SearchItem{Model: brokerFact(""), Page: 1, Size: 100, SkipCount: true}, &rows))
	for _, r := range rows {
		require.NotEqual(t, "effect:C1", r.EventID)
	}
	s.flag("allow:C1")
	s.wait("C1", "completed")
	s.wait("C2", "completed")
	require.Eventually(t, func() bool { return s.pending() == 0 }, 5*time.Second, 10*time.Millisecond)
	s.flag("stop:" + strconv.Itoa(a.Process.Pid))
	require.NoError(t, a.Wait())
	s.publish("E", "E1")
	require.Equal(t, b.Process.Pid, s.wait("E1", "completed").PID)
	t.Logf("真实消费者PID=%d,%d，同subject/group，失败与正常退出恢复通过", a.Process.Pid, b.Process.Pid)
}

// TestBrokerOnlyPublisherCrashBeforeOutboxConfirmation 硬杀在Broker成功后、Badger确认前，重开原卷形成同EventID重复，正式Inbox事务仅一份效果。
func TestBrokerOnlyPublisherCrashBeforeOutboxConfirmation(t *testing.T) {
	s := newBrokerProcessSuite(t)
	s.start("consumer", false)
	p := s.start("publisher", true)
	require.Eventually(t, func() bool { return s.client.Exists(context.Background(), s.prefix+":mark-entered").Val() == 1 }, 5*time.Second, 10*time.Millisecond)
	s.wait("duplicate", "completed")
	require.NoError(t, p.Process.Kill())
	require.Error(t, p.Wait())
	s.start("publisher", false)
	require.Eventually(t, func() bool { return s.client.Exists(context.Background(), s.prefix+":marked").Val() == 1 }, 5*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		n := 0
		for _, r := range s.records() {
			if r.Body == "duplicate" && r.Phase == "completed" {
				n++
			}
		}
		return n == 2 && s.pending() == 0
	}, 5*time.Second, 10*time.Millisecond)
	require.EqualValues(t, 2, s.client.XLen(context.Background(), s.prefix+":events").Val(), "原Broker记录保留，不能用删组/重置制造幂等")
	var rows []*brokerInboxFact
	require.NoError(t, s.db.Load(&pt.SearchItem{Model: brokerFact(""), Page: 1, Size: 100, SkipCount: true}, &rows))
	require.Len(t, rows, 2, "一份Inbox和一份业务效果")
}

// TestBrokerOnlyHardCrashNativeLeaseRecovery 不改变Redis原生租约、消费组或保护标记，等待旧key接管再按序恢复。
func TestBrokerOnlyHardCrashNativeLeaseRecovery(t *testing.T) {
	if os.Getenv("CORE_TEST_SHARED_FULL_LEASE") != "1" {
		t.Skip("NOT_RUN: CORE_TEST_SHARED_FULL_LEASE=1，等待原生租约")
	}
	s := newBrokerProcessSuite(t)
	a := s.start("consumer", false)
	s.publish("A", "A1")
	s.wait("A1", "entered")
	b := s.start("consumer", false)
	killed := time.Now()
	require.NoError(t, a.Process.Kill())
	require.Error(t, a.Wait())
	s.flag("release:A1")
	s.publish("A", "A2")
	s.publish("B", "B1")
	s.wait("B1", "completed")
	require.Eventually(t, func() bool {
		for _, r := range s.records() {
			if r.Body == "A2" && r.Phase == "completed" {
				return true
			}
		}
		return false
	}, 150*time.Second, 20*time.Millisecond)
	var order []string
	for _, r := range s.records() {
		if r.Key == "A" && r.Phase == "completed" {
			require.Equal(t, b.Process.Pid, r.PID)
			order = append(order, r.Body)
		}
	}
	require.Equal(t, []string{"A1", "A2"}, order)
	require.Eventually(t, func() bool { return s.pending() == 0 }, 5*time.Second, 10*time.Millisecond)
	require.EqualValues(t, 3, s.client.XLen(context.Background(), s.prefix+":events").Val())
	t.Logf("硬杀PID=%d，接管PID=%d，未加速租约恢复=%s", a.Process.Pid, b.Process.Pid, time.Since(killed))
}
