package mq

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newNATSReliableProvider(t *testing.T) (*NATSJetStreamProvider, context.Context) {
	t.Helper()
	url := os.Getenv("CORE_TEST_NATS_URL")
	if url == "" {
		t.Skip("NOT RUN: 设置 CORE_TEST_NATS_URL 后运行 NATS JetStream 可靠订阅真 Broker 测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	prefix := fmt.Sprintf("coretest%d", time.Now().UnixNano())
	provider := NewNATSJetStreamProvider(url, prefix, prefix)
	require.NoError(t, provider.Connect(ctx))
	t.Cleanup(func() {
		_ = provider.Close()
		cancel()
	})
	return provider, ctx
}

func natsReliablePolicy(subject, dlq string, max int, groups ...string) LifecyclePolicy {
	required := make([]ConsumerGroupRequirement, 0, len(groups))
	for _, group := range groups {
		required = append(required, ConsumerGroupRequirement{Name: group, Start: StartFromAllRetained})
	}
	return LifecyclePolicy{
		Subject: subject, RequiredGroups: required,
		Retry: RetryPolicy{
			MaxDeliveries: max, DeadLetterSubject: dlq,
			Backoff: []time.Duration{30 * time.Millisecond}, MaxAckPending: 10,
		},
	}.Normalize()
}

func TestNATSReliableSubscribersUseIndependentDurables(t *testing.T) {
	provider, ctx := newNATSReliableProvider(t)
	policy := natsReliablePolicy("fills", "fills.dlq", 3, "users", "positions")
	users := make(chan string, 1)
	positions := make(chan string, 1)
	for group, output := range map[string]chan string{"users": users, "positions": positions} {
		options := ReliableSubscribeOptions{Group: group, Consumer: group + "-a", lifecycle: &policy}
		cancel, err := provider.SubscribeReliable(ctx, policy.Subject, options, func(message *Message) error {
			output <- string(message.Data)
			return nil
		})
		require.NoError(t, err)
		defer cancel()
	}
	require.NoError(t, provider.Publish(ctx, policy.Subject, []byte("fill-1"), nil))
	require.Equal(t, "fill-1", <-users)
	require.Equal(t, "fill-1", <-positions)
}

func TestNATSReliableHandlerFailureRedeliversAfterBackoff(t *testing.T) {
	provider, ctx := newNATSReliableProvider(t)
	policy := natsReliablePolicy("fills", "fills.dlq", 3, "positions")
	var attempts atomic.Int32
	times := make(chan time.Time, 2)
	done := make(chan struct{}, 1)
	cancel, err := provider.SubscribeReliable(ctx, policy.Subject, ReliableSubscribeOptions{
		Group: "positions", Consumer: "positions-a", lifecycle: &policy,
	}, func(*Message) error {
		times <- time.Now()
		if attempts.Add(1) == 1 {
			return errors.New("temporary")
		}
		done <- struct{}{}
		return nil
	})
	require.NoError(t, err)
	defer cancel()
	require.NoError(t, provider.Publish(ctx, policy.Subject, []byte("fill-1"), nil))
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("message was not redelivered")
	}
	first, second := <-times, <-times
	require.GreaterOrEqual(t, second.Sub(first), 25*time.Millisecond)
	require.Equal(t, int32(2), attempts.Load())
}

func TestNATSReliableRestartContinuesFromDurableState(t *testing.T) {
	provider, ctx := newNATSReliableProvider(t)
	policy := natsReliablePolicy("fills", "fills.dlq", 3, "positions")
	first := make(chan string, 1)
	firstCancel, err := provider.SubscribeReliable(ctx, policy.Subject, ReliableSubscribeOptions{
		Group: "positions", Consumer: "positions-a", lifecycle: &policy,
	}, func(message *Message) error {
		first <- string(message.Data)
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, provider.Publish(ctx, policy.Subject, []byte("fill-1"), nil))
	require.Equal(t, "fill-1", <-first)
	consumer, err := provider.js.Consumer(ctx,
		natsResourceName(provider.streamPrefix, policy.Subject),
		natsResourceName(provider.durablePrefix, policy.Subject+"-positions"),
	)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		info, infoErr := consumer.Info(ctx)
		return infoErr == nil && info.NumAckPending == 0
	}, time.Second, 10*time.Millisecond)
	firstCancel()
	require.NoError(t, provider.Close())
	restarted := NewNATSJetStreamProvider(provider.url, provider.streamPrefix, provider.durablePrefix)
	require.NoError(t, restarted.Connect(ctx))
	t.Cleanup(func() { _ = restarted.Close() })

	require.NoError(t, restarted.Publish(ctx, policy.Subject, []byte("fill-2"), nil))
	second := make(chan string, 1)
	secondCancel, err := restarted.SubscribeReliable(ctx, policy.Subject, ReliableSubscribeOptions{
		Group: "positions", Consumer: "positions-b", lifecycle: &policy,
	}, func(message *Message) error {
		second <- string(message.Data)
		return nil
	})
	require.NoError(t, err)
	defer secondCancel()
	select {
	case body := <-second:
		require.Equal(t, "fill-2", body)
	case <-ctx.Done():
		t.Fatal("durable consumer did not continue after restart")
	}
}

func TestNATSReliableAtLimitPublishesDLQBeforeTerm(t *testing.T) {
	provider, ctx := newNATSReliableProvider(t)
	policy := natsReliablePolicy("fills", "fills.dlq", 2, "positions")
	cancel, err := provider.SubscribeReliable(ctx, policy.Subject, ReliableSubscribeOptions{
		Group: "positions", Consumer: "positions-a", lifecycle: &policy,
	}, func(*Message) error { return errors.New("poison") })
	require.NoError(t, err)
	defer cancel()
	require.NoError(t, provider.Publish(ctx, policy.Subject, []byte("fill-1"), nil))

	dlqStream, err := provider.js.Stream(ctx, natsResourceName(provider.streamPrefix, policy.Retry.DeadLetterSubject))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		info, infoErr := dlqStream.Info(ctx)
		return infoErr == nil && info.State.Msgs == 1
	}, 3*time.Second, 20*time.Millisecond)
	consumer, err := provider.js.Consumer(ctx,
		natsResourceName(provider.streamPrefix, policy.Subject),
		natsResourceName(provider.durablePrefix, policy.Subject+"-positions"),
	)
	require.NoError(t, err)
	info, err := consumer.Info(ctx)
	require.NoError(t, err)
	require.Zero(t, info.NumAckPending)
	raw, err := dlqStream.GetLastMsgForSubject(ctx, provider.subjectKey(policy.Retry.DeadLetterSubject))
	require.NoError(t, err)
	require.Equal(t, []byte("fill-1"), raw.Data)
	require.Equal(t, policy.Subject, raw.Header.Get("Core-Original-Subject"))
	require.Equal(t, "positions", raw.Header.Get("Core-Consumer-Group"))
}

func TestNATSReliableDLQPublishFailureDoesNotTerminateOriginal(t *testing.T) {
	provider, ctx := newNATSReliableProvider(t)
	policy := natsReliablePolicy("fills", "fills.dlq", 1, "positions")
	cancel, err := provider.SubscribeReliable(ctx, policy.Subject, ReliableSubscribeOptions{
		Group: "positions", Consumer: "positions-a", lifecycle: &policy,
	}, func(*Message) error { return errors.New("poison") })
	require.NoError(t, err)
	defer cancel()
	require.NoError(t, provider.js.DeleteStream(ctx, natsResourceName(provider.streamPrefix, policy.Retry.DeadLetterSubject)))
	require.NoError(t, provider.Publish(ctx, policy.Subject, []byte("fill-1"), nil))
	consumer, err := provider.js.Consumer(ctx,
		natsResourceName(provider.streamPrefix, policy.Subject),
		natsResourceName(provider.durablePrefix, policy.Subject+"-positions"),
	)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		info, infoErr := consumer.Info(ctx)
		return infoErr == nil && info.NumAckPending > 0
	}, 2*time.Second, 20*time.Millisecond)
}

func TestNATSReliableMaxAckPendingAppliesBrokerBackpressure(t *testing.T) {
	provider, ctx := newNATSReliableProvider(t)
	policy := natsReliablePolicy("fills", "fills.dlq", 3, "positions")
	policy.Retry.MaxAckPending = 1
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	defer close(release)
	cancel, err := provider.SubscribeReliable(ctx, policy.Subject, ReliableSubscribeOptions{
		Group: "positions", Consumer: "positions-a", lifecycle: &policy,
	}, func(*Message) error {
		started <- struct{}{}
		<-release
		return nil
	})
	require.NoError(t, err)
	defer cancel()
	require.NoError(t, provider.Publish(ctx, policy.Subject, []byte("fill-1"), nil))
	require.NoError(t, provider.Publish(ctx, policy.Subject, []byte("fill-2"), nil))
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("first handler was not started")
	}
	select {
	case <-started:
		t.Fatal("MaxAckPending=1 时 Broker 不应投递第二条消息")
	case <-time.After(150 * time.Millisecond):
	}
	consumer, err := provider.js.Consumer(ctx,
		natsResourceName(provider.streamPrefix, policy.Subject),
		natsResourceName(provider.durablePrefix, policy.Subject+"-positions"),
	)
	require.NoError(t, err)
	info, err := consumer.Info(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, info.NumAckPending)
}
