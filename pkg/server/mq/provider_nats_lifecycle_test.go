package mq

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

func natsLifecyclePolicy(subject string, mode LifecycleMode, batch int, groups ...ConsumerGroupRequirement) LifecyclePolicy {
	return LifecyclePolicy{
		Subject: subject, Mode: mode, RequiredPublishAck: PublishAckBrokerPersisted,
		RequiredGroups: groups,
		Reclaim:        ReclaimBudget{Interval: time.Hour, BatchSize: batch, TimeBudget: time.Second},
	}
}

// TestNATSLifecycleReensurePreservesAdvancedFrontier 验证重启不能把已推进的前沿误判为策略变更。
func TestNATSLifecycleReensurePreservesAdvancedFrontier(t *testing.T) {
	for _, start := range []ConsumerStartPosition{StartFromAllRetained, StartFromNew} {
		t.Run(string(start), func(t *testing.T) {
			p, ctx := newNATSReliableProvider(t)
			policy := natsLifecyclePolicy("restart", LifecycleModeEnforce, 2, ConsumerGroupRequirement{Name: "required", Start: start})
			require.NoError(t, p.EnsureLifecycle(ctx, policy))
			stop, err := p.SubscribeReliable(ctx, policy.Subject, ReliableSubscribeOptions{Group: "required", lifecycle: &policy}, func(*Message) error { return nil })
			require.NoError(t, err)
			defer stop()
			require.NoError(t, p.Publish(ctx, policy.Subject, []byte("completed"), nil))
			require.Eventually(t, func() bool { s, e := p.InspectLifecycle(ctx, policy); return e == nil && s.SafeFrontier != "0" }, time.Second, 10*time.Millisecond)
			require.NoError(t, p.EnsureLifecycle(ctx, policy))
		})
	}
}

// TestNATSLifecycleNoRequiredGroups 验证无 durable 时按保留期有界回收，出现 durable 后停止。
func TestNATSLifecycleNoRequiredGroups(t *testing.T) {
	provider, ctx := newNATSReliableProvider(t)
	policy := natsLifecyclePolicy("history", LifecycleModeEnforce, 2)
	policy.NoRequiredGroups = true
	policy.Retention.MinAge = 100 * time.Millisecond
	require.NoError(t, provider.EnsureLifecycle(ctx, policy))
	for i := 0; i < 3; i++ {
		require.NoError(t, provider.Publish(ctx, policy.Subject, []byte("history"), nil))
	}
	snapshot, err := provider.InspectLifecycle(ctx, policy)
	require.NoError(t, err)
	result, err := provider.ReclaimLifecycle(ctx, policy, snapshot)
	require.NoError(t, err)
	require.Zero(t, result.Reclaimed)
	time.Sleep(120 * time.Millisecond)
	result, err = provider.ReclaimLifecycle(ctx, policy, snapshot)
	require.NoError(t, err)
	require.EqualValues(t, 2, result.Reclaimed)
	stream, err := provider.js.Stream(ctx, natsResourceName(provider.streamPrefix, policy.Subject))
	require.NoError(t, err)
	_, err = stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{Durable: "unexpected", AckPolicy: jetstream.AckExplicitPolicy})
	require.NoError(t, err)
	_, err = provider.ReclaimLifecycle(ctx, policy, snapshot)
	require.ErrorIs(t, err, ErrLifecycleStateUncertain)
	_, err = provider.InspectLifecycle(ctx, policy)
	require.ErrorIs(t, err, ErrLifecycleStateUncertain)
	require.ErrorIs(t, provider.EnsureLifecycle(ctx, policy), ErrLifecycleStateUncertain)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, info.State.Msgs)
}

// TestNATSLifecycleNoRequiredGroupsRejectsBeforeEnrollment 验证拒绝既有消费者不会写入错误指纹。
func TestNATSLifecycleNoRequiredGroupsRejectsBeforeEnrollment(t *testing.T) {
	provider, ctx := newNATSReliableProvider(t)
	subject := "existing-history"
	_, err := provider.js.CreateStream(ctx, jetstream.StreamConfig{Name: natsResourceName(provider.streamPrefix, subject), Subjects: []string{provider.subjectKey(subject)}})
	require.NoError(t, err)
	require.NoError(t, provider.Publish(ctx, subject, []byte("retained"), nil))
	stream, err := provider.js.Stream(ctx, natsResourceName(provider.streamPrefix, subject))
	require.NoError(t, err)
	_, err = stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{Durable: "existing", AckPolicy: jetstream.AckExplicitPolicy})
	require.NoError(t, err)
	policy := natsLifecyclePolicy(subject, LifecycleModeObserve, 2)
	policy.NoRequiredGroups = true
	policy.Retention.MinAge = time.Hour
	require.ErrorIs(t, provider.EnsureLifecycle(ctx, policy), ErrLifecycleStateUncertain)
	store, err := provider.natsLifecycleStore(ctx, provider.js)
	require.NoError(t, err)
	_, err = store.Get(ctx, provider.natsLifecycleFingerprintKey(subject))
	require.ErrorIs(t, err, jetstream.ErrKeyNotFound)
}

func TestNATSLifecyclePrecreatesOfflineRequiredDurables(t *testing.T) {
	provider, ctx := newNATSReliableProvider(t)
	policy := natsLifecyclePolicy("fills", LifecycleModeObserve, 10,
		ConsumerGroupRequirement{Name: "online", Start: StartFromAllRetained},
		ConsumerGroupRequirement{Name: "offline", Start: StartFromAllRetained},
	)
	require.NoError(t, provider.EnsureLifecycle(ctx, policy))
	streamName := natsResourceName(provider.streamPrefix, policy.Subject)
	for _, group := range []string{"online", "offline"} {
		_, err := provider.js.Consumer(ctx, streamName, natsResourceName(provider.durablePrefix, policy.Subject+"-"+group))
		require.NoError(t, err)
	}
}

func TestNATSLifecycleOfflineRequiredDurableBlocksReclaim(t *testing.T) {
	provider, ctx := newNATSReliableProvider(t)
	policy := natsLifecyclePolicy("fills", LifecycleModeEnforce, 10,
		ConsumerGroupRequirement{Name: "online", Start: StartFromAllRetained},
		ConsumerGroupRequirement{Name: "offline", Start: StartFromAllRetained},
	)
	require.NoError(t, provider.EnsureLifecycle(ctx, policy))
	done := make(chan struct{}, 1)
	cancel, err := provider.SubscribeReliable(ctx, policy.Subject, ReliableSubscribeOptions{
		Group: "online", Consumer: "online-a", lifecycle: &policy,
	}, func(*Message) error { done <- struct{}{}; return nil })
	require.NoError(t, err)
	defer cancel()
	require.NoError(t, provider.Publish(ctx, policy.Subject, []byte("fill-1"), nil))
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("online durable did not consume")
	}
	snapshot, err := provider.InspectLifecycle(ctx, policy)
	require.NoError(t, err)
	result, err := provider.ReclaimLifecycle(ctx, policy, snapshot)
	require.NoError(t, err)
	require.Zero(t, result.Reclaimed)
	stream, err := provider.js.Stream(ctx, natsResourceName(provider.streamPrefix, policy.Subject))
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), info.State.Msgs)
}

func TestNATSLifecycleAllDurablesPermitBoundedReclaim(t *testing.T) {
	provider, ctx := newNATSReliableProvider(t)
	policy := natsLifecyclePolicy("fills", LifecycleModeEnforce, 2,
		ConsumerGroupRequirement{Name: "users", Start: StartFromAllRetained},
		ConsumerGroupRequirement{Name: "positions", Start: StartFromAllRetained},
	)
	require.NoError(t, provider.EnsureLifecycle(ctx, policy))
	done := make(chan struct{}, 6)
	for _, group := range []string{"users", "positions"} {
		cancel, err := provider.SubscribeReliable(ctx, policy.Subject, ReliableSubscribeOptions{
			Group: group, Consumer: group + "-a", lifecycle: &policy,
		}, func(*Message) error { done <- struct{}{}; return nil })
		require.NoError(t, err)
		defer cancel()
	}
	for i := 0; i < 3; i++ {
		require.NoError(t, provider.Publish(ctx, policy.Subject, []byte("fill"), nil))
	}
	for i := 0; i < 6; i++ {
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatal("required durable did not consume all messages")
		}
	}
	require.Eventually(t, func() bool {
		snapshot, inspectErr := provider.InspectLifecycle(ctx, policy)
		return inspectErr == nil && snapshot.SafeFrontier == "3"
	}, time.Second, 10*time.Millisecond)
	snapshot, err := provider.InspectLifecycle(ctx, policy)
	require.NoError(t, err)
	result, err := provider.ReclaimLifecycle(ctx, policy, snapshot)
	require.NoError(t, err)
	require.Equal(t, int64(2), result.Reclaimed)
}

func TestNATSLifecycleStartFromNewSkipsExistingHistory(t *testing.T) {
	provider, ctx := newNATSReliableProvider(t)
	subject := "fills"
	_, err := provider.js.CreateStream(ctx, jetstream.StreamConfig{
		Name: natsResourceName(provider.streamPrefix, subject), Subjects: []string{provider.subjectKey(subject)},
	})
	require.NoError(t, err)
	require.NoError(t, provider.Publish(ctx, subject, []byte("history"), nil))
	policy := natsLifecyclePolicy(subject, LifecycleModeObserve, 10,
		ConsumerGroupRequirement{Name: "new-reader", Start: StartFromNew},
	)
	require.NoError(t, provider.EnsureLifecycle(ctx, policy))
	got := make(chan string, 2)
	cancel, err := provider.SubscribeReliable(ctx, subject, ReliableSubscribeOptions{
		Group: "new-reader", Consumer: "new-reader-a", lifecycle: &policy,
	}, func(message *Message) error { got <- string(message.Data); return nil })
	require.NoError(t, err)
	defer cancel()
	require.NoError(t, provider.Publish(ctx, subject, []byte("new"), nil))
	select {
	case body := <-got:
		require.Equal(t, "new", body)
	case <-ctx.Done():
		t.Fatal("new-only durable did not receive new message")
	}
}

func TestNATSLifecycleRejectsUnsafeExistingStreamLimits(t *testing.T) {
	provider, ctx := newNATSReliableProvider(t)
	subject := "fills"
	_, err := provider.js.CreateStream(ctx, jetstream.StreamConfig{
		Name: natsResourceName(provider.streamPrefix, subject), Subjects: []string{provider.subjectKey(subject)},
		MaxAge: time.Minute,
	})
	require.NoError(t, err)
	policy := natsLifecyclePolicy(subject, LifecycleModeEnforce, 10,
		ConsumerGroupRequirement{Name: "positions", Start: StartFromAllRetained},
	)
	require.ErrorIs(t, provider.EnsureLifecycle(ctx, policy), ErrLifecycleUnsafeBrokerPolicy)
}

func TestNATSLifecycleMissingRequiredDurableFailsClosed(t *testing.T) {
	provider, ctx := newNATSReliableProvider(t)
	policy := natsLifecyclePolicy("fills", LifecycleModeEnforce, 10,
		ConsumerGroupRequirement{Name: "positions", Start: StartFromAllRetained},
	)
	require.NoError(t, provider.EnsureLifecycle(ctx, policy))
	require.NoError(t, provider.js.DeleteConsumer(ctx,
		natsResourceName(provider.streamPrefix, policy.Subject),
		natsResourceName(provider.durablePrefix, policy.Subject+"-positions"),
	))
	_, err := provider.InspectLifecycle(ctx, policy)
	require.ErrorIs(t, err, ErrLifecycleStateUncertain)
}

func TestNATSLifecycleMinAgeKeepsCompletedMessage(t *testing.T) {
	provider, ctx := newNATSReliableProvider(t)
	policy := natsLifecyclePolicy("fills", LifecycleModeEnforce, 10,
		ConsumerGroupRequirement{Name: "positions", Start: StartFromAllRetained},
	)
	policy.Retention.MinAge = time.Hour
	require.NoError(t, provider.EnsureLifecycle(ctx, policy))
	done := make(chan struct{}, 1)
	cancel, err := provider.SubscribeReliable(ctx, policy.Subject, ReliableSubscribeOptions{
		Group: "positions", Consumer: "positions-a", lifecycle: &policy,
	}, func(*Message) error { done <- struct{}{}; return nil })
	require.NoError(t, err)
	defer cancel()
	require.NoError(t, provider.Publish(ctx, policy.Subject, []byte("fill-1"), nil))
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("required durable did not consume")
	}
	require.Eventually(t, func() bool {
		snapshot, inspectErr := provider.InspectLifecycle(ctx, policy)
		return inspectErr == nil && snapshot.SafeFrontier == "1"
	}, time.Second, 10*time.Millisecond)
	snapshot, err := provider.InspectLifecycle(ctx, policy)
	require.NoError(t, err)
	result, err := provider.ReclaimLifecycle(ctx, policy, snapshot)
	require.NoError(t, err)
	require.Zero(t, result.Reclaimed)
}

func TestNATSLifecycleRecreatedDurableCannotRegressCompletedFrontier(t *testing.T) {
	provider, ctx := newNATSReliableProvider(t)
	policy := natsLifecyclePolicy("fills", LifecycleModeObserve, 10,
		ConsumerGroupRequirement{Name: "positions", Start: StartFromAllRetained},
	)
	require.NoError(t, provider.EnsureLifecycle(ctx, policy))
	done := make(chan struct{}, 1)
	cancel, err := provider.SubscribeReliable(ctx, policy.Subject, ReliableSubscribeOptions{
		Group: "positions", Consumer: "positions-a", lifecycle: &policy,
	}, func(*Message) error { done <- struct{}{}; return nil })
	require.NoError(t, err)
	require.NoError(t, provider.Publish(ctx, policy.Subject, []byte("fill-1"), nil))
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("required durable did not consume")
	}
	require.Eventually(t, func() bool {
		snapshot, inspectErr := provider.InspectLifecycle(ctx, policy)
		return inspectErr == nil && snapshot.SafeFrontier == "1"
	}, time.Second, 10*time.Millisecond)
	cancel()
	streamName := natsResourceName(provider.streamPrefix, policy.Subject)
	durable := natsResourceName(provider.durablePrefix, policy.Subject+"-positions")
	require.NoError(t, provider.js.DeleteConsumer(ctx, streamName, durable))
	stream, err := provider.js.Stream(ctx, streamName)
	require.NoError(t, err)
	_, err = stream.CreateOrUpdateConsumer(ctx, provider.natsLifecycleConsumerConfig(policy, policy.RequiredGroups[0]))
	require.NoError(t, err)

	_, err = provider.InspectLifecycle(ctx, policy)
	require.ErrorIs(t, err, ErrLifecycleStateUncertain)
}

func TestNATSLifecycleDeclaredHardCapacityRejectsNewPublish(t *testing.T) {
	provider, ctx := newNATSReliableProvider(t)
	policy := natsLifecyclePolicy("fills", LifecycleModeObserve, 10,
		ConsumerGroupRequirement{Name: "positions", Start: StartFromAllRetained},
	)
	policy.Capacity.HardMessages = 1
	require.NoError(t, provider.EnsureLifecycle(ctx, policy))
	require.NoError(t, provider.Publish(ctx, policy.Subject, []byte("fill-1"), nil))
	require.Error(t, provider.Publish(ctx, policy.Subject, []byte("fill-2"), nil))
	stream, err := provider.js.Stream(ctx, natsResourceName(provider.streamPrefix, policy.Subject))
	require.NoError(t, err)
	info, err := stream.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), info.State.Msgs)
}

func TestNATSLifecycleConcurrentPublishAckAndReclaimDoesNotLoseMessages(t *testing.T) {
	provider, ctx := newNATSReliableProvider(t)
	policy := natsLifecyclePolicy("fills", LifecycleModeEnforce, 7,
		ConsumerGroupRequirement{Name: "positions", Start: StartFromAllRetained},
	)
	require.NoError(t, provider.EnsureLifecycle(ctx, policy))
	const total = 100
	consumed := make(map[string]int, total)
	var consumedMu sync.Mutex
	done := make(chan struct{}, total)
	cancel, err := provider.SubscribeReliable(ctx, policy.Subject, ReliableSubscribeOptions{
		Group: "positions", Consumer: "positions-a", lifecycle: &policy,
	}, func(message *Message) error {
		consumedMu.Lock()
		consumed[string(message.Data)]++
		consumedMu.Unlock()
		done <- struct{}{}
		return nil
	})
	require.NoError(t, err)
	defer cancel()

	published := make(chan error, 1)
	go func() {
		for i := 0; i < total; i++ {
			if publishErr := provider.Publish(ctx, policy.Subject, []byte(fmt.Sprintf("fill-%03d", i)), nil); publishErr != nil {
				published <- publishErr
				return
			}
		}
		published <- nil
	}()
	reclaimDone := make(chan struct{})
	go func() {
		defer close(reclaimDone)
		for i := 0; i < 40; i++ {
			snapshot, inspectErr := provider.InspectLifecycle(ctx, policy)
			if inspectErr == nil {
				_, _ = provider.ReclaimLifecycle(ctx, policy, snapshot)
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()
	for i := 0; i < total; i++ {
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatalf("only consumed %d/%d messages", i, total)
		}
	}
	require.NoError(t, <-published)
	<-reclaimDone
	consumedMu.Lock()
	defer consumedMu.Unlock()
	require.Len(t, consumed, total)
	for body, count := range consumed {
		require.Equal(t, 1, count, "message %s delivery count", body)
	}
}
