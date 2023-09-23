package example

import (
	"context"
	"fmt"
	"github.com/digitalwayhk/core/pkg/dec"
	"github.com/digitalwayhk/core/pkg/dec/subscribe"
	"math/rand"
	"strconv"
	"testing"
	"time"
)

var ctx = context.Background()
var eventContent = map[string]interface{}{"key1": "value1"}
var random = rand.New(rand.NewSource(time.Now().Unix()))

// TestBatchPublish 批量任务发布测试 .  100协程处理1W结果：10S处理完成，100同步，其他异步
func TestBatchPublish(t *testing.T) {
	event1 := "TestBatchPublish"
	eventList := []string{event1}
	subscribers := make([]subscribe.Subscriber, 2)
	subscribers[0] = newSayHello(eventList, 0, nil, true)
	subscribers[1] = newSayHello(eventList, 0, nil, true)
	updateSayHelloExecuteTime(subscribers, 100)

	err := dec.Client.RegisterSubscriber(subscribers)
	if err != nil {
		println(err)
		t.Fail()
	}
	dec.ClientConfig.CoprocessorCount = 100

	for i := 0; i < 10000; i++ {
		//创建事件
		ctx1 := dec.Client.CreateEventContext(ctx, domain, "", event1, eventContent)
		//发布事件
		dec.Client.PublishEvent(ctx1)
	}
	time.Sleep(time.Second * 1)
}

func TestPublishWithDependence(t *testing.T) {
	event1 := "TestPublishWithDependence"
	eventList := []string{event1}
	size := 5
	subscribers := make([]subscribe.Subscriber, size)
	currentIndex := getCurrentIndex()
	subscribers[0] = newSayHello(eventList, 0, nil, true)
	subscribers[1] = newSayHello(eventList, 0, newSayHelloDependence(currentIndex, 0), true)
	subscribers[2] = newSayHello(eventList, 0, newSayHelloDependence(currentIndex, 1), false)
	subscribers[3] = newSayHello(eventList, 0, newSayHelloDependence(currentIndex, 2), true)
	subscribers[4] = newSayHello(eventList, 0, newSayHelloDependence(currentIndex, 1), true)
	err := dec.Client.RegisterSubscriber(subscribers)
	if err != nil {
		fmt.Println(err)
		t.Fail()
		return
	}
	//创建事件
	key := getKey()
	ctx := dec.Client.CreateEventContext(ctx, domain, key, event1, eventContent)

	//发布事件
	err = dec.Client.SyncPublishEvent(ctx)
	if err != nil {
		fmt.Println("PublishEvent err:", err)
		t.Fail()
		return
	}
}

func TestPublishOrdered(t *testing.T) {
	event1 := "TestPublishOrdered"
	eventList := []string{event1}
	size := 5
	subscribers := make([]subscribe.Subscriber, size)
	currentIndex := getCurrentIndex()
	subscribers[0] = newSayHello(eventList, 0, nil, true)
	subscribers[1] = newSayHello(eventList, 0, newSayHelloDependence(currentIndex, 0), true)
	subscribers[2] = newSayHello(eventList, 0, newSayHelloDependence(currentIndex, 1), false)
	subscribers[3] = newSayHello(eventList, 0, newSayHelloDependence(currentIndex, 2), true)
	subscribers[4] = newSayHello(eventList, 0, newSayHelloDependence(currentIndex, 1), true)
	err := dec.Client.RegisterSubscriber(subscribers)
	if err != nil {
		fmt.Println(err)
		t.Fail()
		return
	}
	//创建事件
	key := getKey()
	ctx := dec.Client.CreateEventContext(ctx, domain, key, event1, eventContent)

	//发布事件
	err = dec.Client.PublishOrderedEvent(ctx)
	time.Sleep(time.Minute * 10)
	if err != nil {
		fmt.Println("PublishEvent err:", err)
		t.Fail()
		return
	}
}

func getKey() string {
	return "key_" + strconv.Itoa(random.Intn(100000000))
}
