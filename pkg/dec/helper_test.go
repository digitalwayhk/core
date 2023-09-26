package dec

import (
	"context"
	"github.com/digitalwayhk/core/pkg/dec/subscribe"
	"github.com/digitalwayhk/core/pkg/dec/util"
	"testing"
	"time"
)

type DemoEvent struct {
	IEventData
	Name string
}

func TestAsyncPublishParallelEvent(t *testing.T) {
	err := Client.RegisterSubscriber([]subscribe.Subscriber{demoSubscriber{}})
	if err != nil {
		panic(err)
	}
	AsyncPublishParallelEvent("demo", "demo", DemoEvent{Name: "demo"})
	time.Sleep(time.Minute * 100)
}

func TestAsyncPublishOrderedEvent(t *testing.T) {
	err := Client.RegisterSubscriber([]subscribe.Subscriber{demoSubscriber{}})
	if err != nil {
		panic(err)
	}
	AsyncPublishOrderedEvent("demo", "demo", DemoEvent{Name: "demo"})
	time.Sleep(time.Minute * 100)
}

func TestSyncPublishEvent(t *testing.T) {
	err := Client.RegisterSubscriber([]subscribe.Subscriber{demoSubscriber{}})
	if err != nil {
		panic(err)
	}
	_ = SyncPublishEvent("demo", "demo", DemoEvent{Name: "demo"})
}

type demoSubscriber struct {
	subscribe.Subscriber
}

func (s demoSubscriber) GetConfig() subscribe.Config {
	return *subscribe.NewDomainConfig("panicSubscriber", "demo", "demo")
}

func (s demoSubscriber) Execute(ctx context.Context, req subscribe.Request) subscribe.Result {
	testEvent := GetEventData[DemoEvent](req)
	println(util.JsonUtil.ToString(testEvent))
	return *subscribeResult
}
