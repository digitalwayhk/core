package mq

import (
	"context"
	"fmt"
	"sync"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// NATSJetStreamProvider implements MQProvider using NATS JetStream.
// It is the recommended production provider.
type NATSJetStreamProvider struct {
	url           string
	streamPrefix  string
	durablePrefix string
	mu            sync.Mutex
	conn          *nats.Conn
	js            jetstream.JetStream
	subs          []jetstream.ConsumeContext
}

// NewNATSJetStreamProvider creates a provider connecting to the given NATS URL.
func NewNATSJetStreamProvider(url, streamPrefix, durablePrefix string) *NATSJetStreamProvider {
	if streamPrefix == "" {
		streamPrefix = "digitalway-core"
	}
	if durablePrefix == "" {
		durablePrefix = "core"
	}
	return &NATSJetStreamProvider{
		url:           url,
		streamPrefix:  streamPrefix,
		durablePrefix: durablePrefix,
	}
}

func (n *NATSJetStreamProvider) Name() string { return "nats-jetstream" }

// Connect establishes a NATS connection and creates a JetStream context.
func (n *NATSJetStreamProvider) Connect(_ context.Context) error {
	conn, err := nats.Connect(n.url)
	if err != nil {
		return fmt.Errorf("nats-jetstream: connect to %s: %w", n.url, err)
	}
	js, err := jetstream.New(conn)
	if err != nil {
		conn.Close()
		return fmt.Errorf("nats-jetstream: create JetStream context: %w", err)
	}
	n.mu.Lock()
	n.conn = conn
	n.js = js
	n.mu.Unlock()
	return nil
}

// Close drains and closes the NATS connection.
func (n *NATSJetStreamProvider) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, sub := range n.subs {
		sub.Stop()
	}
	n.subs = nil
	if n.conn != nil {
		n.conn.Drain()
		n.conn = nil
	}
	return nil
}

// Publish publishes data to the JetStream subject.
func (n *NATSJetStreamProvider) Publish(ctx context.Context, subject string, data []byte, opts *PublishOptions) error {
	n.mu.Lock()
	js := n.js
	n.mu.Unlock()
	if js == nil {
		return ErrNotConnected
	}
	pubOpts := []jetstream.PublishOpt{}
	if opts != nil && opts.IdempotencyKey != "" {
		pubOpts = append(pubOpts, jetstream.WithMsgID(opts.IdempotencyKey))
	}
	_, err := js.Publish(ctx, n.subjectKey(subject), data, pubOpts...)
	return err
}

// Subscribe creates or reuses a durable consumer on the stream for subject.
func (n *NATSJetStreamProvider) Subscribe(ctx context.Context, subject string, handler func(*Message)) (func(), error) {
	n.mu.Lock()
	js := n.js
	n.mu.Unlock()
	if js == nil {
		return nil, ErrNotConnected
	}
	streamName := n.streamPrefix + "-" + subject
	durableName := n.durablePrefix + "-" + subject

	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{n.subjectKey(subject)},
	})
	if err != nil {
		return nil, fmt.Errorf("nats-jetstream: create stream %s: %w", streamName, err)
	}
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:   durableName,
		AckPolicy: jetstream.AckExplicitPolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("nats-jetstream: create consumer %s: %w", durableName, err)
	}
	cc, err := consumer.Consume(func(msg jetstream.Msg) {
		m := &Message{
			ID:      msg.Headers().Get("Nats-Msg-Id"),
			Subject: subject,
			Data:    msg.Data(),
			Ack:     msg.Ack,
		}
		handler(m)
	})
	if err != nil {
		return nil, fmt.Errorf("nats-jetstream: consume: %w", err)
	}
	n.mu.Lock()
	n.subs = append(n.subs, cc)
	n.mu.Unlock()
	return cc.Stop, nil
}

// Health checks connectivity to NATS.
func (n *NATSJetStreamProvider) Health(_ context.Context) error {
	n.mu.Lock()
	conn := n.conn
	n.mu.Unlock()
	if conn == nil || !conn.IsConnected() {
		return ErrNotConnected
	}
	return nil
}

func (n *NATSJetStreamProvider) subjectKey(subject string) string {
	return n.streamPrefix + "." + subject
}
