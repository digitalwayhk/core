package eventbus

import (
	"context"
	"errors"
	"github.com/digitalwayhk/core/pkg/dec/subscribe"
	"github.com/digitalwayhk/core/pkg/dec/util"
	"github.com/zeromicro/go-zero/core/logx"

	"strings"
	"sync"
)

var EventBus = newEventBus()

type eventBus struct {
	subscriberList          []subscribe.Subscriber
	subscriberEventMap      map[string][]string
	subscriberEventBatchMap map[string]map[string]int
	subscriberWrapperMap    map[string]subscribe.SubscriberWrapper
}

func newEventBus() *eventBus {
	return &eventBus{
		subscriberList:          make([]subscribe.Subscriber, 0),
		subscriberEventMap:      make(map[string][]string),
		subscriberEventBatchMap: make(map[string]map[string]int),
		subscriberWrapperMap:    make(map[string]subscribe.SubscriberWrapper),
	}
}

func (e *eventBus) GetSubscriberList(domain string, eventCode string) []string {
	key := getEventKey(domain, eventCode)
	return e.subscriberEventMap[key]
}

func (e *eventBus) Register(subscribers []subscribe.Subscriber) error {
	if len(subscribers) == 0 {
		return nil
	}
	//注册subscriber
	for _, subscriber := range subscribers {
		if subscriber == nil {
			continue
		}
		name := subscriber.GetConfig().GetName()
		if name == "" {
			logx.Error("register name is null")
			return errors.New("register name is null")
		}
		//name不能重复
		if _, ok := e.subscriberWrapperMap[name]; ok {
			logx.Error("register name repeat,name:%s", name)
			return errors.New("register name repeat,name:" + name)
		}
		e.subscriberList = append(e.subscriberList, subscriber)
		e.subscriberWrapperMap[name] = *subscribe.NewSubscriberWrapper(subscriber)
	}
	//生成事件Map
	eventMap := buildEventMap(e.subscriberList)
	//生成subscriberEventMap
	e.subscriberEventMap = createSubscriberEventNameMap(eventMap)
	//生成subscriberEventBatchMap
	e.subscriberEventBatchMap = createSubscriberEventBatchMap(eventMap)
	for event, subscriberList := range e.subscriberEventMap {
		batchMap := e.subscriberEventBatchMap[event]
		for _, name := range subscriberList {
			if batchMap[name] <= 0 {
				logx.Error("register loop dependence,name:%s,event:%s", name, event)
				return errors.New("register loop dependence,name:" + name + ",event:" + event)
			}
		}
	}
	logx.Infof("register succ,add:%d,total:%d", len(subscribers), len(e.subscriberList))
	//日志打印
	for _, subscriber := range e.subscriberList {
		logx.Infof("subscriber:%v", subscriber.GetConfig())
	}
	logx.Infof("subscriberEventMap:%s", util.JsonUtil.ToString(e.subscriberEventMap))
	logx.Infof("subscriberEventBatchMap:%s", util.JsonUtil.ToString(e.subscriberEventBatchMap))
	return nil
}

func (e *eventBus) Notify(ctx context.Context, request subscribe.RequestWrapper) {
	//分批
	batchTasks := e.buildBatchTasks(request)
	//按批次执行，批次内并发执行
	for _, tasks := range batchTasks {
		e.batchNotify(ctx, tasks)
	}
}

func (e *eventBus) batchNotify(ctx context.Context, tasks []notifyTask) {
	length := len(tasks)
	if length == 0 {
		return
	}
	if length == 1 {
		task := tasks[0]
		e.notifyWrapper(ctx, task.name, task.request)
		return
	}
	wg := &sync.WaitGroup{}
	wg.Add(length)
	for _, task := range tasks {
		go e.asyncNotify(ctx, task, wg)
	}
	wg.Wait()
}

func (e *eventBus) asyncNotify(ctx context.Context, task notifyTask, wg *sync.WaitGroup) {
	// 从panic中恢复
	defer func() {
		if er := recover(); er != nil {
			logx.Errorf("[PANIC]asyncNotify err,%v\n%s\n", er, util.RuntimeUtil.GetStack())
		}
	}()
	defer wg.Done()
	e.notifyWrapper(ctx, task.name, task.request)
}

func (e *eventBus) notifyWrapper(ctx context.Context, name string, request subscribe.RequestWrapper) {
	if subscriber, ok := e.subscriberWrapperMap[name]; ok {
		subscriber.Execute(ctx, request)
		return
	}
}

func (e *eventBus) buildTask(name string, request subscribe.RequestWrapper) notifyTask {
	return notifyTask{
		name:    name,
		request: request,
	}
}

func (e *eventBus) buildBatchTasks(requestWrapper subscribe.RequestWrapper) [][]notifyTask {
	key := getEventKey(requestWrapper.Request.GetDomain(), requestWrapper.Request.GetEventCode())
	subscribeList := e.subscriberEventMap[key]
	batchMap := e.subscriberEventBatchMap[key]
	maxBatch := 0
	for _, batch := range batchMap {
		maxBatch = max(maxBatch, batch)
	}
	batchTasks := make([][]notifyTask, maxBatch)
	for _, name := range subscribeList {
		task := e.buildTask(name, requestWrapper)
		batch := batchMap[name]
		batchTasks[batch-1] = append(batchTasks[batch-1], task)
	}
	return batchTasks
}

type notifyTask struct {
	name    string
	request subscribe.RequestWrapper
}

func createSubscriberEventNameMap(subscriberEventMap map[string][]subscribe.Subscriber) map[string][]string {
	subscriberEventNameMap := make(map[string][]string, len(subscriberEventMap))
	for key, value := range subscriberEventMap {
		names := make([]string, len(value))
		for i, r := range value {
			names[i] = r.GetConfig().GetName()
		}
		subscriberEventNameMap[key] = names
	}
	return subscriberEventNameMap
}

func createSubscriberEventBatchMap(subscriberEventMap map[string][]subscribe.Subscriber) map[string]map[string]int {
	subscriberEventBatchMap := make(map[string]map[string]int, len(subscriberEventMap))
	for key, value := range subscriberEventMap {
		domain, event := parseEventKey(key)
		batchMap := buildBatchMap(domain, event, value)
		subscriberEventBatchMap[key] = batchMap
	}
	return subscriberEventBatchMap
}

func buildBatchMap(domain string, event string, subscribers []subscribe.Subscriber) map[string]int {
	batchMap := make(map[string]int)
	if len(subscribers) == 0 {
		return batchMap
	}
	var dependenceList []subscribe.Config
	//第一批：无依赖
	for _, r := range subscribers {
		config := r.GetConfig()
		subscribe := config.GetSubscribe(domain, event)
		if subscribe == nil || len(subscribe.Dependence) == 0 {
			batchMap[config.GetName()] = 1
			continue
		}
		dependenceList = append(dependenceList, config)
	}
	//处理依赖
	size := len(dependenceList)
	for i := 0; i < size; i++ {
		oldLen := len(dependenceList)
		if oldLen == 0 {
			break
		}
		dependenceList = handleDependence(domain, event, dependenceList, batchMap)
		newLen := len(dependenceList)
		if newLen >= oldLen {
			logx.Errorf("handleDependence loop or invalid dependence,dependenceList:%v", dependenceList)
			break
		}
	}
	return batchMap
}

func handleDependence(domain string, event string, list []subscribe.Config, batchMap map[string]int) []subscribe.Config {
	var dependenceList []subscribe.Config
	for _, config := range list {
		revolve := 0
		batch := 0
		subscribe := config.GetSubscribe(domain, event)
		for _, dependence := range subscribe.Dependence {
			if b, ok := batchMap[dependence]; ok && b > 0 {
				revolve++
				batch = max(batch, b+1)
			}
		}
		if revolve == len(subscribe.Dependence) {
			batchMap[config.GetName()] = batch
			continue
		}
		dependenceList = append(dependenceList, config)
	}
	return dependenceList
}

func buildEventMap(subscribers []subscribe.Subscriber) map[string][]subscribe.Subscriber {
	subscriberEventMap := make(map[string][]subscribe.Subscriber, len(subscribers))
	for _, subscriber := range subscribers {
		config := subscriber.GetConfig()
		for _, subscribe := range config.GetSubscribeList() {
			key := getEventKey(subscribe.Domain, subscribe.Event)
			subscriberEventMap[key] = append(subscriberEventMap[key], subscriber)
		}
	}
	return subscriberEventMap
}

func getEventKey(domain string, event string) string {
	return domain + ":" + event
}

func parseEventKey(key string) (string, string) {
	result := strings.SplitN(key, ":", 2)
	if len(result) == 2 {
		return result[0], result[1]
	}
	if len(result) == 1 {
		return result[0], ""
	}
	return "", ""
}

func max(i, j int) int {
	if i >= j {
		return i
	}
	return j
}
