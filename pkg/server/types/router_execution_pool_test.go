package types

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/config"
	"github.com/digitalwayhk/core/pkg/server/event"
	"github.com/stretchr/testify/require"
)

type panicExecutionRouter struct{}

func (*panicExecutionRouter) Parse(IRequest) error             { return nil }
func (*panicExecutionRouter) Validation(IRequest) error        { return nil }
func (*panicExecutionRouter) RouterInfo() *RouterInfo          { return nil }
func (*panicExecutionRouter) Do(IRequest) (interface{}, error) { panic("boom") }

func TestRouterInfoExecDoPanicReturnsSafeResponse(t *testing.T) {
	info := &RouterInfo{Path: "/api/test/panic", ServiceName: "test"}
	response := info.ExecDo(&panicExecutionRouter{}, &shardTestRequest{})
	if response == nil {
		t.Fatal("ExecDo 捕获 panic 后必须返回安全错误响应，不能返回 nil")
	}
}

func TestRouterInfoSubscriptionsDelegateToServiceEventBridge(t *testing.T) {
	bridge := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{})
	t.Cleanup(func() { require.NoError(t, bridge.Close(context.Background())) })
	info := &RouterInfo{
		Path:        "/api/test/events",
		ServiceName: "test",
		Subscriber:  make(map[ObserveState]map[string]*ObserveArgs, 3),
	}
	info.SetEventBridge("test", bridge)
	received := make(chan *NotifyArgs, 1)
	observer := &ObserveArgs{
		State:          ObserveResponse,
		ReceiveService: "event-bridge-test",
		CallBack: func(args *NotifyArgs) error {
			received <- args
			return nil
		},
	}
	require.NoError(t, info.Subscribe(observer))

	info.responseNotify(json.RawMessage(`{"value":"parsed"}`), "trace-1", json.RawMessage(`{"ok":true}`))
	select {
	case got := <-received:
		if got.TraceID != "trace-1" {
			t.Fatalf("TraceID = %q, want trace-1", got.TraceID)
		}
	case <-time.After(time.Second):
		t.Fatal("等待 ServiceEventBridge 观察回调超时")
	}

	require.NoError(t, info.UnSubscribe(observer))
	info.responseNotify(nil, "trace-2", nil)
	select {
	case <-received:
		t.Fatal("取消订阅后不应继续收到观察事件")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestRouterInfoEventBridgeRebindsForRecreatedService(t *testing.T) {
	first := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{})
	info := &RouterInfo{
		Path:        "/api/test/rebind",
		ServiceName: "test",
		Subscriber:  make(map[ObserveState]map[string]*ObserveArgs, 3),
	}
	info.SetEventBridge("test", first)
	info.Freeze("test")
	received := make(chan string, 1)
	require.NoError(t, info.Subscribe(&ObserveArgs{
		State:          ObserveResponse,
		ReceiveService: "rebind-test",
		CallBack: func(args *NotifyArgs) error {
			received <- args.TraceID
			return nil
		},
	}))
	require.NoError(t, first.Close(context.Background()))

	second := event.NewServiceEventBridge(event.NewStream(), event.ServiceEventBridgeOptions{})
	t.Cleanup(func() { require.NoError(t, second.Close(context.Background())) })
	info.SetEventBridge("test", second)
	info.responseNotify(nil, "trace-recreated", nil)

	select {
	case traceID := <-received:
		if traceID != "trace-recreated" {
			t.Fatalf("TraceID = %q, want trace-recreated", traceID)
		}
	case <-time.After(time.Second):
		t.Fatal("服务重建后订阅未迁移到新的 ServiceEventBridge")
	}
}

var snapshotTestInfo *RouterInfo

type snapshotExecutionRouter struct {
	Value string
}

func (*snapshotExecutionRouter) RouterInfo() *RouterInfo { return snapshotTestInfo }
func (r *snapshotExecutionRouter) Parse(IRequest) error {
	r.Value = "parsed"
	return nil
}
func (*snapshotExecutionRouter) Validation(IRequest) error        { return nil }
func (*snapshotExecutionRouter) Do(IRequest) (interface{}, error) { return "ok", nil }
func (r *snapshotExecutionRouter) Clean()                         { r.Value = "cleaned" }

func TestRouterInfoObserverUsesSnapshotBeforePoolReturn(t *testing.T) {
	info := &RouterInfo{
		Path:        "/api/test/snapshot",
		ServiceName: "test",
		Subscriber:  make(map[ObserveState]map[string]*ObserveArgs, 3),
	}
	snapshotTestInfo = info
	info.SetInstance(&snapshotExecutionRouter{})

	release := make(chan struct{})
	observed := make(chan string, 1)
	err := info.Subscribe(&ObserveArgs{
		State:          ObserveResponse,
		ReceiveService: "snapshot-test",
		CallBack: func(args *NotifyArgs) error {
			<-release
			var value snapshotExecutionRouter
			if err := args.GetInstance(&value); err != nil {
				observed <- "decode-error:" + err.Error()
				return nil
			}
			observed <- value.Value
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	if response := info.Exec(&shardTestRequest{}); response == nil {
		t.Fatal("Exec() response = nil")
	}
	close(release)

	select {
	case value := <-observed:
		if value != "parsed" {
			t.Fatalf("观察回调应读取归还对象池前的快照，得到 %q", value)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("等待观察回调超时")
	}
}

type plainPoolRouter struct {
	marker byte
}

func (*plainPoolRouter) Parse(IRequest) error             { return nil }
func (*plainPoolRouter) Validation(IRequest) error        { return nil }
func (*plainPoolRouter) Do(IRequest) (interface{}, error) { return nil, nil }
func (*plainPoolRouter) RouterInfo() *RouterInfo          { return nil }

func TestChannelPoolGetPutUsesSamePool(t *testing.T) {
	info := &RouterInfo{Path: "/api/test/pool", ServiceName: "test"}
	info.SetInstance(&plainPoolRouter{})

	first := info.New()
	info.putRouter(first)
	second := info.New()

	if first != second {
		t.Fatal("RouterInfo 归还和获取必须使用同一个有界对象池")
	}
}

var resetCalls atomic.Int32
var cleanCalls atomic.Int32

type lifecyclePoolRouter struct{}

func (*lifecyclePoolRouter) Parse(IRequest) error             { return nil }
func (*lifecyclePoolRouter) Validation(IRequest) error        { return nil }
func (*lifecyclePoolRouter) Do(IRequest) (interface{}, error) { return nil, nil }
func (*lifecyclePoolRouter) RouterInfo() *RouterInfo          { return nil }
func (*lifecyclePoolRouter) Reset()                           { resetCalls.Add(1) }
func (*lifecyclePoolRouter) Clean()                           { cleanCalls.Add(1) }

func TestChannelPoolUsesResetAndCleanContracts(t *testing.T) {
	resetCalls.Store(0)
	cleanCalls.Store(0)
	info := &RouterInfo{Path: "/api/test/pool-lifecycle", ServiceName: "test"}
	info.SetInstance(&lifecyclePoolRouter{})

	router := info.New()
	if got := resetCalls.Load(); got != 1 {
		t.Fatalf("首次取出后 Reset 调用次数 = %d，期望 1", got)
	}
	info.putRouter(router)
	if got := cleanCalls.Load(); got != 1 {
		t.Fatalf("归还前 Clean 调用次数 = %d，期望 1", got)
	}
	_ = info.New()
	if got := resetCalls.Load(); got != 2 {
		t.Fatalf("复用取出后 Reset 调用次数 = %d，期望 2", got)
	}
}

var factoryCalls atomic.Int32
var factoryProductCleanCalls atomic.Int32

type factoryPoolRouter struct{}

func (*factoryPoolRouter) Parse(IRequest) error             { return nil }
func (*factoryPoolRouter) Validation(IRequest) error        { return nil }
func (*factoryPoolRouter) Do(IRequest) (interface{}, error) { return nil, nil }
func (*factoryPoolRouter) RouterInfo() *RouterInfo          { return nil }
func (*factoryPoolRouter) New(interface{}) IRouter {
	factoryCalls.Add(1)
	return &factoryPoolProduct{}
}

type factoryPoolProduct struct {
	marker byte
	Count  int
}

func (*factoryPoolProduct) Parse(IRequest) error             { return nil }
func (*factoryPoolProduct) Validation(IRequest) error        { return nil }
func (*factoryPoolProduct) Do(IRequest) (interface{}, error) { return nil, nil }
func (*factoryPoolProduct) RouterInfo() *RouterInfo          { return nil }
func (*factoryPoolProduct) Clean()                           { factoryProductCleanCalls.Add(1) }

func TestChannelPoolPoolsFactoryResult(t *testing.T) {
	factoryCalls.Store(0)
	info := &RouterInfo{Path: "/api/test/factory-pool", ServiceName: "test"}
	info.SetInstance(&factoryPoolRouter{})

	first := info.New()
	info.putRouter(first)
	second := info.New()

	if first != second {
		t.Fatal("IRouterFactory 最终请求实例应由 RouterInfo 对象池复用")
	}
	if got := factoryCalls.Load(); got != 1 {
		t.Fatalf("IRouterFactory.New 调用次数 = %d，期望 1", got)
	}
}

func TestRouterInfoSubscriptionInstanceDoesNotUseRequestPool(t *testing.T) {
	factoryCalls.Store(0)
	factoryProductCleanCalls.Store(0)
	info := &RouterInfo{Path: "/api/test/subscription", ServiceName: "test"}
	info.SetInstance(&factoryPoolRouter{})

	subscription := info.NewSubscription()
	info.releaseSubscription(subscription)
	request := info.New()

	if subscription == request {
		t.Fatal("WebSocket 订阅实例不得进入短期请求对象池")
	}
	if got := factoryCalls.Load(); got != 2 {
		t.Fatalf("IRouterFactory.New 调用次数 = %d，期望订阅和请求各创建一次", got)
	}
	if got := factoryProductCleanCalls.Load(); got != 1 {
		t.Fatalf("订阅释放时 Clean 调用次数 = %d，期望 1", got)
	}
}

func TestRouterInfoParseSubscriptionFailureCleansAndDropsInstance(t *testing.T) {
	factoryCalls.Store(0)
	factoryProductCleanCalls.Store(0)
	info := &RouterInfo{Path: "/api/test/subscription-parse-failure", ServiceName: "test"}
	info.SetInstance(&factoryPoolRouter{})

	_, err := info.ParseSubscription(map[string]interface{}{"Count": "invalid"})
	require.Error(t, err)
	request := info.New()

	if got := factoryProductCleanCalls.Load(); got != 1 {
		t.Fatalf("订阅解析失败时 Clean 调用次数 = %d，期望 1", got)
	}
	if got := factoryCalls.Load(); got != 2 {
		t.Fatalf("订阅解析失败后请求应创建新实例，工厂调用次数 = %d，期望 2", got)
	}
	require.NotNil(t, request)
}

func TestRouterInfoParseNewFailureReturnsRequestToPool(t *testing.T) {
	factoryCalls.Store(0)
	info := &RouterInfo{Path: "/api/test/parse-failure", ServiceName: "test"}
	info.SetInstance(&factoryPoolRouter{})

	_, err := info.ParseNew(map[string]interface{}{"Count": "invalid"})
	require.Error(t, err)
	_ = info.New()

	if got := factoryCalls.Load(); got != 1 {
		t.Fatalf("解析失败后 IRouterFactory.New 调用次数 = %d，期望已取得实例被归还", got)
	}
}

func TestRouterInfoExecDoReturnsRequestToPool(t *testing.T) {
	info := &RouterInfo{Path: "/api/test/exec-do", ServiceName: "test"}
	info.SetInstance(&plainPoolRouter{})

	router := info.New()
	response := info.ExecDo(router, &shardTestRequest{})
	require.NotNil(t, response)
	reused := info.New()

	if reused != router {
		t.Fatal("ExecDo 完成后必须归还短期请求实例")
	}
}

func TestRouterInfoExecReturnsRequestToPoolDuringInitialization(t *testing.T) {
	config.BeginServerInitialization()
	t.Cleanup(config.EndServerInitialization)
	factoryCalls.Store(0)
	info := &RouterInfo{Path: "/api/test/initializing-exec", ServiceName: "test"}
	info.SetInstance(&factoryPoolRouter{})

	require.NotNil(t, info.Exec(&shardTestRequest{}))
	require.NotNil(t, info.New())

	require.Equal(t, int32(1), factoryCalls.Load(), "初始化期 Exec 完成后应归还请求实例")
}

var initializationParseFactoryCalls atomic.Int32

type initializationParseFactoryRouter struct{}

func (*initializationParseFactoryRouter) Parse(IRequest) error             { return nil }
func (*initializationParseFactoryRouter) Validation(IRequest) error        { return nil }
func (*initializationParseFactoryRouter) Do(IRequest) (interface{}, error) { return nil, nil }
func (*initializationParseFactoryRouter) RouterInfo() *RouterInfo          { return nil }
func (*initializationParseFactoryRouter) New(interface{}) IRouter {
	initializationParseFactoryCalls.Add(1)
	return &initializationParseFailureRouter{}
}

type initializationParseFailureRouter struct{}

func (*initializationParseFailureRouter) Parse(IRequest) error { return errors.New("parse failed") }
func (*initializationParseFailureRouter) Validation(IRequest) error {
	return nil
}
func (*initializationParseFailureRouter) Do(IRequest) (interface{}, error) { return nil, nil }
func (*initializationParseFailureRouter) RouterInfo() *RouterInfo          { return nil }

func TestRouterInfoExecParseFailureReturnsRequestToPoolDuringInitialization(t *testing.T) {
	config.BeginServerInitialization()
	t.Cleanup(config.EndServerInitialization)
	initializationParseFactoryCalls.Store(0)
	info := &RouterInfo{Path: "/api/test/initializing-parse-failure", ServiceName: "test"}
	info.SetInstance(&initializationParseFactoryRouter{})

	require.NotNil(t, info.Exec(&shardTestRequest{}))
	require.NotNil(t, info.New())

	require.Equal(t, int32(1), initializationParseFactoryCalls.Load(), "初始化期 Parse 失败后应归还请求实例")
}

func TestRouterInfoExecRejectsFrozenMetadataMutationBeforeCreatingRequest(t *testing.T) {
	factoryCalls.Store(0)
	info := &RouterInfo{
		Path:        "/api/test/frozen-exec",
		ServiceName: "test",
		Method:      "POST",
		PathType:    PublicType,
	}
	info.SetInstance(&factoryPoolRouter{})
	info.Freeze("test")
	info.Auth = true

	require.PanicsWithValue(t, "router metadata changed after registration", func() {
		info.Exec(&shardTestRequest{})
	})
	require.Zero(t, factoryCalls.Load(), "冻结校验必须发生在创建请求实例之前")
}

func TestRouterInfoExecDoRejectsFrozenMetadataMutationAndReturnsRequest(t *testing.T) {
	const originalPath = "/api/test/frozen-exec-do"
	info := &RouterInfo{
		Path:        originalPath,
		ServiceName: "test",
		Method:      "POST",
		PathType:    PublicType,
	}
	info.SetInstance(&plainPoolRouter{})
	info.Freeze("test")
	requestRouter := info.New()
	info.Path = "/api/test/changed"

	require.PanicsWithValue(t, "router metadata changed after registration", func() {
		info.ExecDo(requestRouter, &shardTestRequest{})
	})

	info.Path = originalPath
	require.Same(t, requestRouter, info.New(), "ExecDo 冻结校验失败后仍应归还请求实例")
}
