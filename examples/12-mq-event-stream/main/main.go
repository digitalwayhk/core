package main

import (
	"context"
	"fmt"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/digitalwayhk/core/pkg/server/mq"
	"github.com/digitalwayhk/core/pkg/server/router"
	"github.com/digitalwayhk/core/pkg/server/types"
)

// fakeProvider 是本地演示用 MQ provider，Publish 会同步调用 Subscribe 的 handler。
type fakeProvider struct {
	handler func(*mq.Message)
}

func (own *fakeProvider) Name() string                  { return "demo-fake" }
func (own *fakeProvider) Connect(context.Context) error { return nil }
func (own *fakeProvider) Close() error                  { return nil }
func (own *fakeProvider) Health(context.Context) error  { return nil }
func (own *fakeProvider) Publish(ctx context.Context, subject string, data []byte, opts *mq.PublishOptions) error {
	if own.handler != nil {
		own.handler(&mq.Message{Subject: subject, Data: data, Ack: func() error { return nil }})
	}
	return nil
}
func (own *fakeProvider) Subscribe(ctx context.Context, subject string, handler func(*mq.Message)) (func(), error) {
	own.handler = handler
	return func() { own.handler = nil }, nil
}

// EventDemoService 只用于创建 ServiceContext。
type EventDemoService struct{}

func (own *EventDemoService) ServiceName() string                    { return "eventdemo" }
func (own *EventDemoService) Routers() []types.IRouter               { return nil }
func (own *EventDemoService) SubscribeRouters() []*types.ObserveArgs { return nil }

func main() {
	const providerName = "demo-fake"

	// 注册一个本地 fake provider，演示自定义 provider 或测试 provider 的接入方式。
	mq.RegisterProviderFactory(providerName, func(ctx context.Context, cfg *config.MQConfig) (mq.MQProvider, error) {
		return &fakeProvider{}, nil
	})

	cfg := config.NewServiceDefaultConfig("eventdemo", 18112)
	cfg.MQ.Mode = "on"
	cfg.MQ.Provider = providerName
	cfg.MQ.Usage = []string{"event-stream"}

	// NewServiceContextWithConfig 会初始化 MQManager，并在 usage 包含 event-stream 时启用 EventBridge。
	sc := router.NewServiceContextWithConfig(&EventDemoService{}, cfg)

	received := make(chan *event.Envelope, 1)
	cancelLocal, _ := sc.EventStream.Subscribe("demo.created", func(env *event.Envelope) {
		received <- env
	})
	defer cancelLocal()

	cancelMQ, err := sc.EventBridge.Subscribe(context.Background(), "demo.events")
	if err != nil {
		panic(err)
	}
	defer cancelMQ()

	env := event.NewEnvelope("examples/12-mq-event-stream", "demo.created", []byte(`{"hello":"event"}`))
	if err := sc.EventBridge.Publish(context.Background(), "demo.events", env); err != nil {
		panic(err)
	}

	select {
	case got := <-received:
		fmt.Printf("收到事件：id=%s type=%s data=%s\n", got.ID, got.Type, string(got.Data))
	case <-time.After(2 * time.Second):
		panic("等待事件超时")
	}
}
