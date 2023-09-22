package dec

import (
	"context"
	"fmt"
	"github.com/digitalwayhk/core/pkg/dec/subscribe"
	"github.com/digitalwayhk/core/pkg/dec/util"

	"testing"
	"time"
)

var ctx = context.Background()
var domain = "domain"
var entityId = "entityId"
var eventCode = "eventCode1"
var eventContent = map[string]interface{}{"key1": "value1"}
var subscribeResult = subscribe.NewResult(true, "")

func init() {
	err := Client.RegisterSubscriber([]subscribe.Subscriber{hello{}})
	if err != nil {
		panic(err)
	}
}

func TestAsyncPublishWithTransaction(t *testing.T) {
	//创建事件
	ctx := Client.CreateEventContext(ctx, domain, entityId, eventCode, eventContent)
	//发布事件
	Client.PublishEvent(ctx)
}

func TestPublishWithNoRetry(t *testing.T) {
	//创建事件
	ctx := Client.CreateEventContext(ctx, domain, entityId, eventCode, eventContent)

	//发布事件
	subscribeResult.Success = false
	subscribeResult.Message = "error"
	err := Client.SyncPublishEvent(ctx)
	if err != nil {
		t.Fail()
		return
	}
}

func TestPanicSubscriber(t *testing.T) {
	err := Client.RegisterSubscriber([]subscribe.Subscriber{panicSubscriber{}})
	if err != nil {
		panic(err)
	}
	//创建事件
	ctx := Client.CreateEventContext(ctx, domain, entityId, eventCode, eventContent)
	//发布事件
	Client.PublishEvent(ctx)
	time.Sleep(time.Millisecond * 100)
}

type hello struct {
	subscribe.Subscriber
}

func (s hello) GetConfig() subscribe.Config {
	return *subscribe.NewDomainConfig("subscriber1", domain, eventCode)
}

func (s hello) Execute(ctx context.Context, req subscribe.Request) subscribe.Result {
	fmt.Printf("Hello,req:%v,config:%s\n", req, util.JsonUtil.ToString(s.GetConfig()))
	//time.Sleep(time.Millisecond * 1000)
	return *subscribeResult
}

type panicSubscriber struct {
	subscribe.Subscriber
}

func (s panicSubscriber) GetConfig() subscribe.Config {
	return *subscribe.NewDomainConfig("panicSubscriber", domain, eventCode)
}

func (s panicSubscriber) Execute(ctx context.Context, req subscribe.Request) subscribe.Result {
	panic("illegal execute.")
	return *subscribeResult
}
