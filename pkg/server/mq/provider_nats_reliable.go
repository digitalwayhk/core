package mq

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/zeromicro/go-zero/core/logx"
)

// SubscribeReliable 使用逻辑消费组创建持久 durable。只有 handler 成功后才确认；
// 有界失败先等待 DLQ publish ACK，再终止原消息重投。
func (n *NATSJetStreamProvider) SubscribeReliable(
	ctx context.Context,
	subject string,
	options ReliableSubscribeOptions,
	handler func(*Message) error,
) (func(), error) {
	n.mu.Lock()
	js := n.js
	n.mu.Unlock()
	if js == nil {
		return nil, ErrNotConnected
	}
	if options.Group == "" || handler == nil {
		return nil, fmt.Errorf("nats-jetstream: reliable group and handler are required")
	}
	policy, err := normalizeNATSReliablePolicy(subject, options)
	if err != nil {
		return nil, err
	}
	streamName := natsResourceName(n.streamPrefix, subject)
	stream, err := js.Stream(ctx, streamName)
	if errors.Is(err, jetstream.ErrStreamNotFound) {
		stream, err = js.CreateStream(ctx, jetstream.StreamConfig{
			Name: streamName, Subjects: []string{n.subjectKey(subject)},
		})
	}
	if err != nil {
		return nil, fmt.Errorf("nats-jetstream: create reliable stream %s: %w", streamName, err)
	}
	if policy != nil && policy.Retry.MaxDeliveries > 0 {
		dlqName := natsResourceName(n.streamPrefix, policy.Retry.DeadLetterSubject)
		_, err = js.Stream(ctx, dlqName)
		if errors.Is(err, jetstream.ErrStreamNotFound) {
			_, err = js.CreateStream(ctx, jetstream.StreamConfig{
				Name: dlqName, Subjects: []string{n.subjectKey(policy.Retry.DeadLetterSubject)},
			})
		}
		if err != nil {
			return nil, fmt.Errorf("nats-jetstream: create dead letter stream: %w", err)
		}
	}

	consumerConfig := jetstream.ConsumerConfig{
		Durable:       natsResourceName(n.durablePrefix, subject+"-"+options.Group),
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
		MaxDeliver:    -1,
	}
	if policy != nil {
		if policy.Retry.MaxAckPending > 0 {
			consumerConfig.MaxAckPending = policy.Retry.MaxAckPending
		}
		for _, required := range policy.RequiredGroups {
			if required.Name == options.Group && required.Start == StartFromNew {
				consumerConfig.DeliverPolicy = jetstream.DeliverNewPolicy
			}
		}
	}
	consumer, err := stream.CreateOrUpdateConsumer(ctx, consumerConfig)
	if err != nil {
		return nil, fmt.Errorf("nats-jetstream: create reliable consumer %s: %w", consumerConfig.Durable, err)
	}

	var handlerMu sync.Mutex
	consumeContext, err := consumer.Consume(func(message jetstream.Msg) {
		handlerMu.Lock()
		defer handlerMu.Unlock()
		n.handleReliableMessage(ctx, js, subject, options.Group, policy, message, handler)
	})
	if err != nil {
		return nil, fmt.Errorf("nats-jetstream: consume reliable: %w", err)
	}
	n.mu.Lock()
	n.subs = append(n.subs, consumeContext)
	n.mu.Unlock()
	return consumeContext.Stop, nil
}

func normalizeNATSReliablePolicy(subject string, options ReliableSubscribeOptions) (*LifecyclePolicy, error) {
	if options.lifecycle == nil {
		return nil, nil
	}
	policy := options.lifecycle.Normalize()
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if policy.Subject != subject {
		return nil, fmt.Errorf("%w: reliable subject %q does not match lifecycle subject %q", ErrLifecyclePolicyInvalid, subject, policy.Subject)
	}
	for _, required := range policy.RequiredGroups {
		if required.Name == options.Group {
			return &policy, nil
		}
	}
	return nil, fmt.Errorf("%w: subject %q group %q", ErrLifecycleRequiredGroupMismatch, subject, options.Group)
}

func (n *NATSJetStreamProvider) handleReliableMessage(
	ctx context.Context,
	js jetstream.JetStream,
	subject, group string,
	policy *LifecyclePolicy,
	message jetstream.Msg,
	handler func(*Message) error,
) {
	metadata, err := message.Metadata()
	if err != nil {
		_ = message.NakWithDelay(50 * time.Millisecond)
		return
	}
	if metadata.NumDelivered > 1 {
		n.lifecycleMetrics.redelivered.Add(1)
	}
	maxDeliveries := 0
	timeout := time.Duration(0)
	if policy != nil {
		maxDeliveries = policy.Retry.MaxDeliveries
		timeout = policy.Retry.HandlerTimeout
	}
	if maxDeliveries == 0 || metadata.NumDelivered <= uint64(maxDeliveries) {
		handlerMessage := &Message{
			ID: message.Headers().Get("Nats-Msg-Id"), Subject: subject,
			Data: append([]byte(nil), message.Data()...),
		}
		if maxDeliveries == 0 {
			handlerMessage.Ack = message.Ack
		}
		err = callNATSReliableHandler(ctx, timeout, message, handler, handlerMessage)
		if err == nil {
			if ackErr := message.DoubleAck(ctx); ackErr != nil {
				logx.Errorw("mq_nats_reliable_ack_failed", logx.Field("subject", subject), logx.Field("error", ackErr))
			}
			return
		}
		if maxDeliveries == 0 || metadata.NumDelivered < uint64(maxDeliveries) {
			_ = message.NakWithDelay(natsRetryDelay(policy, metadata.NumDelivered))
			return
		}
	}

	if err := n.publishNATSDeadLetter(ctx, js, subject, group, policy, metadata, message); err != nil {
		n.lifecycleMetrics.deadLetterFailed.Add(1)
		logx.Errorw("mq_nats_dead_letter_failed", logx.Field("subject", subject), logx.Field("error", err))
		_ = message.NakWithDelay(natsRetryDelay(policy, metadata.NumDelivered))
		return
	}
	n.lifecycleMetrics.deadLetters.Add(1)
	if err := message.Term(); err != nil {
		logx.Errorw("mq_nats_term_failed", logx.Field("subject", subject), logx.Field("error", err))
	}
}

func callNATSReliableHandler(
	ctx context.Context,
	timeout time.Duration,
	brokerMessage jetstream.Msg,
	handler func(*Message) error,
	message *Message,
) error {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = brokerMessage.InProgress()
			}
		}
	}()
	err := callReliableHandler(ctx, timeout, handler, message)
	close(done)
	return err
}

func natsRetryDelay(policy *LifecyclePolicy, attempt uint64) time.Duration {
	if policy == nil {
		return 50 * time.Millisecond
	}
	return redisRetryDelay(policy.Retry, int64(attempt))
}

func (n *NATSJetStreamProvider) publishNATSDeadLetter(
	ctx context.Context,
	js jetstream.JetStream,
	subject, group string,
	policy *LifecyclePolicy,
	metadata *jetstream.MsgMetadata,
	source jetstream.Msg,
) error {
	if policy == nil || policy.Retry.MaxDeliveries <= 0 {
		return errors.New("nats-jetstream: bounded retry policy is unavailable")
	}
	dedupeID := natsDeadLetterDedupeID(subject, group, metadata.Sequence.Stream)
	message := &nats.Msg{
		Subject: n.subjectKey(policy.Retry.DeadLetterSubject),
		Data:    append([]byte(nil), source.Data()...),
		Header:  nats.Header{},
	}
	message.Header.Set("Core-Original-Subject", subject)
	message.Header.Set("Core-Consumer-Group", group)
	message.Header.Set("Core-Original-Stream-Sequence", fmt.Sprint(metadata.Sequence.Stream))
	message.Header.Set("Core-Failure-Class", "handler_failed")
	if orderingKey := source.Headers().Get("Core-Ordering-Key"); orderingKey != "" {
		message.Header.Set("Core-Ordering-Key", orderingKey)
	}
	_, err := js.PublishMsg(ctx, message, jetstream.WithMsgID(dedupeID))
	return err
}

func natsDeadLetterDedupeID(subject, group string, streamSequence uint64) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d", subject, group, streamSequence)))
	return hex.EncodeToString(digest[:])
}
