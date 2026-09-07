// 本文件使用 Core NATS 非 queue 广播，并拒绝 JetStream 捕获内部通知主题。
package controlnotify

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type natsTransport struct {
	conn    *nats.Conn
	js      jetstream.JetStream
	sub     *nats.Subscription
	channel string
	failed  atomic.Bool
}

func openNATS(ctx context.Context, cfg config.NATSJetStreamMQConfig, channel string) (*natsTransport, error) {
	x := &natsTransport{channel: channel}
	nc, err := nats.Connect(cfg.URL, nats.Timeout(operationTimeout), nats.NoReconnect(), nats.ReconnectBufSize(-1),
		nats.PingInterval(heartbeatInterval), nats.MaxPingsOutstanding(2),
		nats.ErrorHandler(func(conn *nats.Conn, _ *nats.Subscription, _ error) {
			x.failed.Store(true)
			// 异步权限/慢消费者错误也必须唤醒阻塞接收，不能等下一条消息才失效。
			conn.Close()
		}),
		nats.DisconnectErrHandler(func(_ *nats.Conn, _ error) { x.failed.Store(true) }),
		nats.ClosedHandler(func(_ *nats.Conn) { x.failed.Store(true) }))
	if err != nil {
		return nil, err
	}
	x.conn = nc
	x.js, err = jetstream.New(nc)
	if err == nil {
		err = x.checkNoStream(ctx)
	}
	if err == nil {
		x.sub, err = nc.SubscribeSync(channel)
	}
	if err == nil {
		err = x.sub.SetPendingLimits(256, 1<<20)
	}
	if err == nil {
		err = nc.FlushWithContext(ctx)
	}
	if err == nil && x.failed.Load() {
		err = ErrUnavailable
	}
	if err != nil {
		nc.Close()
		return nil, err
	}
	return x, nil
}

func (x *natsTransport) checkNoStream(ctx context.Context) error {
	_, err := x.js.StreamNameBySubject(ctx, x.channel)
	if errors.Is(err, jetstream.ErrStreamNotFound) {
		return nil
	}
	if err != nil {
		return errors.New("internal notification JetStream isolation cannot be verified")
	}
	return errors.New("internal notification subject is captured by JetStream")
}

func (x *natsTransport) Publish(ctx context.Context, data []byte) error {
	if err := validatePayload(data); err != nil {
		return err
	}
	if x.failed.Load() || !x.conn.IsConnected() {
		return ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	// 管理面仍须禁止并发创建捕获此命名空间的 Stream；查询与发布不是事务。
	if err := x.checkNoStream(ctx); err != nil {
		x.failed.Store(true)
		return err
	}
	if err := x.conn.Publish(x.channel, data); err != nil {
		x.failed.Store(true)
		return ErrUnavailable
	}
	if err := x.conn.FlushWithContext(ctx); err != nil {
		x.failed.Store(true)
		return ErrUnavailable
	}
	if x.failed.Load() {
		return ErrUnavailable
	}
	return nil
}

func (x *natsTransport) Receive(ctx context.Context) ([]byte, error) {
	if x.failed.Load() || !x.conn.IsConnected() {
		return nil, ErrUnavailable
	}
	msg, err := x.sub.NextMsgWithContext(ctx)
	if err != nil {
		x.failed.Store(true)
		return nil, ErrUnavailable
	}
	if x.failed.Load() || validatePayload(msg.Data) != nil {
		x.failed.Store(true)
		return nil, ErrUnavailable
	}
	return msg.Data, nil
}

func (x *natsTransport) Close() error {
	x.failed.Store(true)
	x.conn.Close()
	return nil
}
