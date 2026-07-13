package types

import (
	"sync/atomic"
	"testing"
	"time"
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
		State:      ObserveResponse,
		OwnAddress: "snapshot-test",
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
}

func (*factoryPoolProduct) Parse(IRequest) error             { return nil }
func (*factoryPoolProduct) Validation(IRequest) error        { return nil }
func (*factoryPoolProduct) Do(IRequest) (interface{}, error) { return nil, nil }
func (*factoryPoolProduct) RouterInfo() *RouterInfo          { return nil }

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
