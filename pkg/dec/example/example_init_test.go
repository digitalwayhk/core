package example

import (
	"fmt"
	"github.com/digitalwayhk/core/pkg/dec"
	"github.com/digitalwayhk/core/pkg/dec/subscribe"

	"testing"
)

// TestRegisterSubscriber1 测试循环依赖
func TestRegisterSubscriber1(t *testing.T) {
	eventList := []string{"TestRegisterSubscriber1"}
	size := 2
	subscribers := make([]subscribe.Subscriber, size)
	currentIndex := getCurrentIndex()
	subscribers[0] = newSayHello(eventList, 0, newSayHelloDependence(currentIndex, 1), true)
	subscribers[1] = newSayHello(eventList, 0, newSayHelloDependence(currentIndex, 0), true)
	err := dec.Client.RegisterSubscriber(subscribers)
	if err == nil {
		t.Fail()
		return
	}
	fmt.Printf("TestRegisterSubscriber1 finish,err:%v\n", err)
}

// TestPublishWithTransaction2 测试错误依赖
func TestRegisterSubscriber2(t *testing.T) {
	eventList := []string{"TestRegisterSubscriber2"}
	size := 3
	subscribers := make([]subscribe.Subscriber, size)
	currentIndex := getCurrentIndex()
	subscribers[0] = newSayHello(eventList, 0, newSayHelloDependence(currentIndex, 2), true)
	subscribers[1] = newSayHello(eventList, 0, newSayHelloDependence(currentIndex, 3), true)
	subscribers[2] = newSayHello(eventList, 0, nil, true)
	err := dec.Client.RegisterSubscriber(subscribers)
	if err != nil {
		t.Fail()
		return
	}
	fmt.Printf("TestRegisterSubscriber2 finish,err:%v\n", err)
}

func TestRegisterSubscriber3(t *testing.T) {
	eventList := []string{"TestRegisterSubscriber2"}
	size := 3
	subscribers := make([]subscribe.Subscriber, size)
	currentIndex := getCurrentIndex()
	subscribers[0] = newSayHello(eventList, 0, newSayHelloDependence(currentIndex, 2), true)
	subscribers[1] = newSayHello(eventList, 0, newSayHelloDependence(currentIndex, 3), true)
	subscribers[2] = newSayHello(eventList, 0, nil, true)
	err := dec.Client.RegisterSubscriber(subscribers)
	if err != nil {
		t.Fail()
		return
	}
	fmt.Printf("TestRegisterSubscriber2 finish,err:%v\n", err)
}
