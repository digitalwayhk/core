package example

import (
	"context"
	"fmt"
	"github.com/digitalwayhk/core/pkg/dec"
	"github.com/digitalwayhk/core/pkg/dec/subscribe"
	"github.com/digitalwayhk/core/pkg/dec/util"

	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	domain = "Dex"
)

var gIndex uint32 = 0
var executeTimeMap = sync.Map{}
var resultMap = sync.Map{}

type sayHello struct {
	config      subscribe.Config
	executeTime int
	result      subscribe.Result
	subscribe.Subscriber
}

func newSayHello(event []string, maxRetryTimes int, dependence []string, res bool) *sayHello {
	config := subscribe.NewConfig(getConfigName("SayHello"), subscribe.NewDomainSubscribe(domain, event, dependence, maxRetryTimes))
	result := subscribe.NewResult(res, config.GetName())
	return &sayHello{
		config: *config,
		result: *result,
	}
}

func newSayHelloDependence(currentIndex int, indexes ...int) []string {
	dependence := make([]string, len(indexes))
	for i, index := range indexes {
		dependence[i] = "SayHello" + strconv.Itoa(currentIndex+index)
	}
	return dependence
}

func updateSayHelloExecuteTime(subscribers []subscribe.Subscriber, executeTime int) {
	for _, subscriber := range subscribers {
		if s, ok := subscriber.(sayHello); ok {
			executeTimeMap.Store(s.config.GetName(), executeTime)
			continue
		}
		if s, ok := subscriber.(*sayHello); ok {
			executeTimeMap.Store(s.config.GetName(), executeTime)
			continue
		}
	}
}

func updateSayHelloResult(subscribers []subscribe.Subscriber, res bool) {
	for _, subscriber := range subscribers {
		if s, ok := subscriber.(sayHello); ok {
			resultMap.Store(s.config.GetName(), res)
			continue
		}
		if s, ok := subscriber.(*sayHello); ok {
			resultMap.Store(s.config.GetName(), res)
			continue
		}
	}
}

func (s sayHello) GetConfig() subscribe.Config {
	return s.config
}

func (s sayHello) Execute(ctx context.Context, request subscribe.Request) subscribe.Result {
	executeTime := s.executeTime
	if t, ok := executeTimeMap.Load(s.config.GetName()); ok {
		if ti, okk := t.(int); okk {
			executeTime = ti
		}
	}
	if executeTime > 0 {
		time.Sleep(time.Millisecond * time.Duration(executeTime))
	}
	result := s.result
	if r, ok := resultMap.Load(s.config.GetName()); ok {
		if tb, okk := r.(bool); okk {
			result.Success = tb
		}
	}
	fmt.Printf(s.GetConfig().GetName()+":req:%v,result:%s\n", request, util.JsonUtil.ToString(result))
	return result
}

type contextValidator struct {
	config subscribe.Config
	subscribe.Subscriber
}

func newContextValidator(event []string) *contextValidator {
	return &contextValidator{
		config: *subscribe.NewDomainConfig(getConfigName("contextValidator"), domain, event...),
	}
}

func (s contextValidator) GetConfig() subscribe.Config {
	return s.config
}

func (s contextValidator) Execute(ctx context.Context, request subscribe.Request) subscribe.Result {
	event := dec.Client.GetEvent(ctx)
	if event == nil {
		return *subscribe.NewResult(true, "")
	}
	fmt.Printf(s.GetConfig().GetName()+":req:%v,ctx:%v\n", request, ctx)
	return *subscribe.NewResult(false, "")
}

func getConfigName(name string) string {
	return name + strconv.Itoa(int(atomic.AddUint32(&gIndex, 1))-1)
}

func getCurrentIndex() int {
	return int(gIndex)
}
