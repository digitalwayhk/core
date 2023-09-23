package dec

import (
	"context"
	"github.com/digitalwayhk/core/pkg/dec/eventbus"
	"github.com/digitalwayhk/core/pkg/dec/publish"
	"github.com/digitalwayhk/core/pkg/dec/subscribe"
	"github.com/digitalwayhk/core/pkg/dec/util"
	"github.com/zeromicro/go-zero/core/logx"
	"sync"

	"github.com/pkg/errors"
)

const (
	created   = 1
	saved     = 2
	published = 3
)

const (
	eventContextKey         = "dec_event"
	defaultCoprocessorCount = 200
)

var ContextNotExistError = errors.New("context not exist")
var IllegalStateError = errors.New("illegal state")

type eventWrapper struct {
	event *publish.Event
	state int
}

func newEventWrapper(event *publish.Event) *eventWrapper {
	return &eventWrapper{
		event: event,
		state: created,
	}
}

var Client = newClient()
var ClientConfig = newClientConfig()

type client struct {
	eventChMap       sync.Map
	eventConsumerMap sync.Map
	mu               sync.Mutex
}

func newClient() *client {
	return &client{}
}

type clientConfig struct {
	CoprocessorCount int `json:"coprocessor_count"`
	ch               chan struct{}
}

func newClientConfig() *clientConfig {
	c := &clientConfig{
		CoprocessorCount: defaultCoprocessorCount,
		ch:               buildChan(defaultCoprocessorCount),
	}

	return c
}

func buildChan(count int) chan struct{} {
	c := make(chan struct{}, count)
	for i := 0; i < count; i++ {
		c <- struct{}{}
	}
	return c
}

// RegisterSubscriber 注册订阅者
func (c client) RegisterSubscriber(subscribers []subscribe.Subscriber) error {
	err := eventbus.EventBus.Register(subscribers)
	//错误报警
	if err != nil {
		logx.Errorf("RegisterSubscriber error,err:%v", err)
	}
	return err
}

// CreateEventContext 创建事件上下文
func (c client) CreateEventContext(ctx context.Context, domain string, entityId string, eventCode string, eventContent map[string]interface{}) context.Context {
	event := publish.Publisher.NewEvent(domain, entityId, eventCode, eventContent)
	wrapper := newEventWrapper(event)
	newCtx := context.WithValue(ctx, eventContextKey, wrapper)
	return newCtx
}

// GetEvent 获取事件
func (c client) GetEvent(ctx context.Context) *publish.Event {
	wrapper := getEventWrapper(ctx)
	if wrapper == nil {
		return nil
	}
	return wrapper.event
}

// UpdateEventContext 更新事件上下文
func (c client) UpdateEventContext(ctx context.Context, entityId string, eventContent map[string]interface{}) error {
	wrapper := getEventWrapper(ctx)
	if wrapper == nil {
		logx.Errorf("UpdateEventContext event context is not exist,entityId:%s,eventContent:%v", entityId, eventContent)
		return ContextNotExistError
	}
	//状态校验
	if wrapper.state != created {
		logx.Errorf("UpdateEventContext state is not created,entityId:%s,eventContent:%v", entityId, eventContent)
		return IllegalStateError
	}
	event := wrapper.event
	if entityId != "" {
		event.SetEntityId(entityId)
	}
	if len(eventContent) > 0 {
		for k, v := range eventContent {
			event.SetParam(k, v)
		}
	}
	return nil
}

// PublishEvent 发布事件，默认异步
func (c client) PublishEvent(ctx context.Context) {
	ch := ClientConfig.ch
	select {
	case <-ch:
		//异步
		go c.asyncPublish(ctx, ch)
	default:
		//超过协程总数时，同步发布事件
		c.syncPublish(ctx)
		//超过协程总数时报警
		logx.Info("Client_CoprocessorExceed")
	}
}

// PublishOrderedEvent 发布有序事件,通过chan消费
func (c client) PublishOrderedEvent(ctx context.Context) error {
	err := c.publishToChan(ctx)
	//错误报警
	if err != nil {
		logx.Errorf("Client_PublishOrderedEvent error,err:%v", err)
	}
	return err
}

// SyncPublishEvent 同步发布事件
func (c client) SyncPublishEvent(ctx context.Context) error {
	err := c.publish(ctx)
	//错误报警
	if err != nil {
		logx.Errorf("Client_SyncPublishEvent error,err:%v", err)
	}
	return err
}

func (c client) publish(ctx context.Context) error {
	wrapper := getEventWrapper(ctx)
	if wrapper == nil {
		logx.Errorf("SyncPublishEvent event context is not exist")
		return ContextNotExistError
	}
	//状态校验
	if wrapper.state == published {
		logx.Errorf("SyncPublishEvent state is published,event:%v", wrapper)
		return IllegalStateError
	}
	//发布事件
	event := wrapper.event
	result := publish.Publisher.PublishEvent(ctx, *event)
	logx.Infof("SyncPublishEvent result:%v", util.JsonUtil.ToString(result))
	wrapper.state = published
	return nil
}

func (c client) publishToChan(ctx context.Context) error {
	wrapper := getEventWrapper(ctx)
	if wrapper == nil {
		logx.Errorf("publishToChan event context is not exist")
		return ContextNotExistError
	}
	//状态校验
	if wrapper.state == published {
		logx.Errorf("publishToChan state is published,event:%v", wrapper)
		return IllegalStateError
	}
	//发布事件
	event := wrapper.event
	eventChan := c.GetEventChan(event.GetEventCode())
	c.initEventChanConsumer(event.GetEventCode(), eventChan)
	eventChan <- event
	wrapper.state = published
	return nil
}

func (c client) asyncPublish(ctx context.Context, ch chan struct{}) {
	// 从panic中恢复
	defer func() {
		if e := recover(); e != nil {
			logx.Errorf("[PANIC]asyncPublish err,%v\n%s\n", e, util.RuntimeUtil.GetStack())
		}
	}()
	// 通知
	defer func() {
		ch <- struct{}{}
	}()
	err := c.publish(ctx)
	//错误报警
	if err != nil {
		logx.Errorf("Client_AsyncPublish error,err:%v", err)
	}
}

func (c client) syncPublish(ctx context.Context) {
	err := c.publish(ctx)
	//错误报警
	if err != nil {
		logx.Errorf("Client_SyncPublish error,err:%v", err)
	}
}

func getEventWrapper(ctx context.Context) *eventWrapper {
	if ctx == nil {
		return nil
	}
	value := ctx.Value(eventContextKey)
	if value == nil {
		return nil
	}
	if wrapper, ok := value.(*eventWrapper); ok {
		return wrapper
	}
	return nil
}

func (e client) GetEventChan(eventCode string) chan *publish.Event {
	//缓存存在
	if value, ok := e.eventChMap.Load(eventCode); ok {
		return value.(chan *publish.Event)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	//二次确认
	if value, ok := e.eventChMap.Load(eventCode); ok {
		return value.(chan *publish.Event)
	}
	eventChan := make(chan *publish.Event)
	//保存
	e.eventChMap.Store(eventCode, eventChan)
	return eventChan
}

func (e client) initEventChanConsumer(eventCode string, eventChan chan *publish.Event) {
	//缓存存在
	if init, ok := e.eventConsumerMap.Load(eventCode); ok {
		if init.(bool) {
			return
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	//二次确认
	if init, ok := e.eventConsumerMap.Load(eventCode); ok {
		if init.(bool) {
			return
		}
	}
	//开启协程监听event
	go func() {
		for {
			select {
			case event := <-eventChan:
				if event == nil {
					continue
				}
				logx.Infof("new event, eventCode:%s EventContent: %s", event.GetEventCode(), util.JsonUtil.ToString(event.GetEventContent()))
				result := publish.Publisher.PublishEvent(context.Background(), *event)
				logx.Infof("SyncPublishEvent result:%v", util.JsonUtil.ToString(result))
			}
		}
	}()
	//保存
	e.eventConsumerMap.Store(eventCode, true)
}
